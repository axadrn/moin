package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

func names(steps []step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.Name)
	}
	return out
}

func TestPlan(t *testing.T) {
	stopPostgres := step{Name: "stop postgres"}
	movePostgres := step{Name: "move postgres"}
	old := daemonSteps
	t.Cleanup(func() { daemonSteps = old })
	daemonSteps = append(slices.Clone(old), daemonStep{
		Before: "v1.0.0-beta.35",
		Steps:  []step{stopPostgres, updateDaemon, movePostgres, startStoppedApps},
	})

	for _, tc := range []struct {
		version string
		want    []step
	}{
		// Newer than every entry: the update alone.
		{"v1.0.0-beta.36", []step{updateDaemon}},
		// Older than one entry: its recipe.
		{"v1.0.0-beta.30", []step{stopPostgres, updateDaemon, movePostgres, startStoppedApps}},
		// Older than both: one recipe around one update, oldest first,
		// and the apps start once.
		{"v1.0.0-beta.20", []step{stopSQLiteApps, stopPostgres, updateDaemon, startStoppedApps, movePostgres}},
		// A version moin cannot read gets every entry.
		{"abc1234", []step{stopSQLiteApps, stopPostgres, updateDaemon, startStoppedApps, movePostgres}},
	} {
		if got := names(plan(tc.version)); !slices.Equal(got, names(tc.want)) {
			t.Errorf("plan(%s) = %v, want %v", tc.version, got, names(tc.want))
		}
	}
}

// stepDaemon answers like a daemon: app.stop with ok, daemon.update with
// ok or an error, and after a good update it comes back as version.
type stepDaemon struct {
	mu      sync.Mutex
	got     []protocol.MessageType
	fail    bool
	version string
}

func (d *stepDaemon) connect(servers *ServerService, server *model.Server, version string) {
	hello := testHello
	hello.DaemonVersion = version
	servers.Connect(server, hello, "203.0.113.5", func() {}, func(message protocol.Message) error {
		d.mu.Lock()
		d.got = append(d.got, message.Type)
		fail := d.fail
		d.mu.Unlock()
		reply := func(messageType protocol.MessageType, data any) {
			answer, _ := protocol.NewMessage(messageType, data)
			answer.RequestID = message.RequestID
			go servers.Receive(server.ID, answer)
		}
		switch message.Type {
		case protocol.TypeAppStop:
			reply(protocol.TypeOK, nil)
		case protocol.TypeDaemonUpdate:
			if fail {
				reply(protocol.TypeError, protocol.Error{Message: "busy"})
				return nil
			}
			reply(protocol.TypeOK, nil)
			go func() {
				time.Sleep(10 * time.Millisecond)
				d.connect(servers, server, d.version)
			}()
		}
		return nil
	})
}

func (d *stepDaemon) sent() []protocol.MessageType {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []protocol.MessageType
	for _, messageType := range d.got {
		if messageType == protocol.TypeAppStop || messageType == protocol.TypeDaemonUpdate || messageType == protocol.TypeDeployStart {
			out = append(out, messageType)
		}
	}
	return out
}

// sqliteApp is a live app named blog with a SQLite database, on a server
// whose daemon is old enough for the beta.26 steps.
func sqliteApp(t *testing.T) (*AppService, *ServerService, *model.Server, *model.App) {
	t.Helper()
	apps, servers, server, _ := newAppService(t)
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	first, err := deployUpload(apps)
	if err != nil {
		t.Fatal(err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true})
	if err := apps.apps.SetDeployDetected(first.ID, `{"databases":[{"path":"/data/app.db"}]}`); err != nil {
		t.Fatal(err)
	}
	app, err := apps.apps.ByIDOrNameForWorkspace("blog", owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	servers.downloads = t.TempDir()
	if err := os.WriteFile(filepath.Join(servers.downloads, build.Name+"-linux-arm64"), []byte("new daemon"), 0o755); err != nil {
		t.Fatal(err)
	}
	return apps, servers, server, app
}

func TestDaemonUpdateStopsSQLiteAppsFirst(t *testing.T) {
	_, servers, server, _ := sqliteApp(t)
	daemon := &stepDaemon{version: "v1.0.0-beta.30"}
	daemon.connect(servers, server, "v1.0.0-beta.20")

	if _, err := servers.UpdateDaemon(context.Background(), owner, "web-1"); err != nil {
		t.Fatal(err)
	}
	want := []protocol.MessageType{protocol.TypeAppStop, protocol.TypeDaemonUpdate, protocol.TypeDeployStart}
	if got := daemon.sent(); !slices.Equal(got, want) {
		t.Fatalf("the daemon got %v, want %v", got, want)
	}
	if note, _ := servers.servers.DaemonUpdate(server.ID); note != nil {
		t.Fatalf("the note stays after the update: %+v", note)
	}
}

func TestDaemonUpdateThatFailsStartsTheAppsAgain(t *testing.T) {
	_, servers, server, _ := sqliteApp(t)
	daemon := &stepDaemon{fail: true}
	daemon.connect(servers, server, "v1.0.0-beta.20")

	_, err := servers.UpdateDaemon(context.Background(), owner, "web-1")
	var stepErr *StepError
	if !errors.As(err, &stepErr) || stepErr.Step != updateDaemon.Name {
		t.Fatalf("err = %v, want the update step", err)
	}
	want := []protocol.MessageType{protocol.TypeAppStop, protocol.TypeDaemonUpdate, protocol.TypeDeployStart}
	if got := daemon.sent(); !slices.Equal(got, want) {
		t.Fatalf("the daemon got %v, want %v", got, want)
	}
	if note, _ := servers.servers.DaemonUpdate(server.ID); note != nil {
		t.Fatalf("all is as before, but the note stays: %+v", note)
	}
	events, _ := servers.activity.ListByWorkspace(owner.WorkspaceID, 50)
	if !slices.ContainsFunc(events, func(event model.ActivityEvent) bool {
		return event.Action == "server.daemon_update_failed"
	}) {
		t.Fatal("the activity has no failed update")
	}
}

func TestDaemonUpdateLeftHalfwayFinishesOnConnect(t *testing.T) {
	apps, servers, server, app := sqliteApp(t)
	if err := apps.apps.SetStopped(app.ID, true); err != nil {
		t.Fatal(err)
	}
	// The hub stopped after the swap: the note says the app is stopped.
	err := servers.servers.SaveDaemonUpdate(model.DaemonUpdate{ServerID: server.ID, FromVersion: "v1.0.0-beta.20", StoppedApps: `["` + app.ID + `"]`, StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	daemon := &stepDaemon{}
	daemon.connect(servers, server, "v1.0.0-beta.30")

	deadline := time.Now().Add(5 * time.Second)
	for !slices.Contains(daemon.sent(), protocol.TypeDeployStart) {
		if time.Now().After(deadline) {
			t.Fatal("the app did not start again")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for {
		if note, _ := servers.servers.DaemonUpdate(server.ID); note == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the note stays")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
