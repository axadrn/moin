package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/protocol"
)

// TestMoments: the quick minutes inside what Every change reaches, then
// the copies and the undos, newest first; a copy moin took before a
// change carries its name.
func TestMoments(t *testing.T) {
	now := time.Date(2026, 11, 7, 14, 30, 0, 0, time.UTC)
	since := now.Add(-2 * time.Hour)
	copyAt := func(at time.Time) protocol.Copy {
		return protocol.Copy{ID: at.Format(protocol.CopyIDLayout), At: at}
	}
	deployed, edited, other := now.Add(-10*time.Minute), now.Add(-40*time.Minute), now.Add(-5*time.Minute)
	backup := protocol.DatabaseBackup{Database: "/data/app.db", Since: &since, Until: &now,
		Copies: []protocol.Copy{copyAt(other), copyAt(deployed), copyAt(now.Add(-20 * time.Minute)), copyAt(edited)},
		Undos:  []protocol.Copy{copyAt(now.Add(-28 * time.Minute))}}
	remembered := []model.BackupMoment{
		{ID: 1, Kind: model.MomentDeploy, Detail: "12", At: deployed},
		{ID: 2, Database: "/data/app.db", Kind: model.MomentEdit, Detail: "sql", At: edited},
		{ID: 3, Database: "/data/other.db", Kind: model.MomentEdit, Detail: "cell:orders", At: other},
	}
	got := []string{}
	for _, moment := range Moments(backup, remembered, now) {
		got = append(got, moment.ID+" "+moment.Label)
	}
	want := []string{
		"1m 1 minute ago", "15m 15 minutes ago", "1h 1 hour ago",
		"20261107T142500Z At 14:25", "before-v12 Before v12", "20261107T141000Z At 14:10", "undo-20261107T140200Z Before the restore at 14:02", "edit-2 Before your SQL change",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("moments\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	moments := Moments(backup, remembered, now)
	if moment, err := findMoment(moments, "BEFORE V12"); err != nil || moment.Copy != deployed.Format(protocol.CopyIDLayout) || moment.Undo != "" {
		t.Errorf("before v12: %+v %v", moment, err)
	}
	if moment, err := findMoment(moments, "undo-20261107T140200Z"); err != nil || moment.Undo != "20261107T140200Z" || moment.Copy != "" {
		t.Errorf("undo: %+v %v", moment, err)
	}

	// A broken Every change reaches only to its newest change.
	newest := now.Add(-20 * time.Minute)
	backup.Until, backup.Error = &newest, "dial tcp: i/o timeout"
	for _, moment := range Moments(backup, remembered, now) {
		if moment.Kind == MomentMinute && moment.At.After(newest) {
			t.Errorf("%s offered after the newest change", moment.ID)
		}
	}
	// Without Every change (Postgres, or switched off) only the copies
	// and the undos.
	backup.Since = nil
	for _, moment := range Moments(backup, remembered, now) {
		if moment.Kind == MomentMinute {
			t.Errorf("a minute without Every change: %+v", moment)
		}
	}
}

func TestBackupProblem(t *testing.T) {
	for raw, want := range map[string]string{
		"": "",
		"copy /data/app.db: dial tcp 1.2.3.4:443: i/o timeout":                                                "The bucket can't be reached.",
		"upload: operation error S3: PutObject, https response error StatusCode: 403, api error AccessDenied": "The bucket refuses the keys.",
		"NoSuchBucket: The specified bucket does not exist":                                                   "The bucket does not exist.",
		"deploy shop once more to start its backups":                                                          "Deploy the app once more to start its backups.",
		"the daily check failed: integrity check: page 4":                                                     "The daily check could not restore the newest copy.",
		"the Postgres service db of shop is not running":                                                      "The database is not running.",
		"write /data/x: no space left on device":                                                              "The server's disk is full.",
		"writing down every change: database is locked":                                                       "Every change is not written down.",
		"something else entirely":                                                                             "The backup does not work.",
	} {
		if got := BackupProblem(raw); got != want {
			t.Errorf("BackupProblem(%q) = %q, want %q", raw, got, want)
		}
	}
}

// backupAgent answers the agent's backup list with one database.
func backupAgent(t *testing.T, apps *AppService, servers *ServerService, server *model.Server, agent *fakeAgent, backup protocol.DatabaseBackup) {
	t.Helper()
	go agentAnswers(servers, server.ID, agent, protocol.TypeBackupList, protocol.TypeBackupsResult, protocol.Backups{App: "shop", Databases: []protocol.DatabaseBackup{backup}})
}

// TestRestoreToMoments: a restore resolves the moment, by ID or label, to
// a copy, an undo or a minute; a time goes to that minute inside Every
// change, else to the newest copy at or before it; a moment that is not
// there is refused before the agent gets anything.
func TestRestoreToMoments(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	app, err := apps.Create(owner, "shop", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	since := now.Add(-3 * time.Hour)
	copied, before := now.Add(-2*time.Hour), now.Add(-30*time.Minute)
	backup := protocol.DatabaseBackup{Database: "/data/app.db", Since: &since, Until: &now,
		Copies: []protocol.Copy{{ID: before.Format(protocol.CopyIDLayout), At: before}, {ID: copied.Format(protocol.CopyIDLayout), At: copied}},
		Undos:  []protocol.Copy{{ID: "20261107T140200Z", At: now.Add(-time.Hour)}}}
	apps.remember(app, "", model.MomentDeploy, "7", before)
	ctx := context.Background()

	restore := func(target RestoreTarget) (protocol.BackupRestore, string, error) {
		t.Helper()
		agent.mu.Lock()
		agent.sent = nil
		agent.mu.Unlock()
		backupAgent(t, apps, servers, server, agent, backup)
		var sent protocol.BackupRestore
		target.Confirm = "shop"
		started, err := apps.StartRestore(ctx, owner, "shop", target)
		if err != nil {
			return sent, "", err
		}
		for deadline := time.Now().Add(5 * time.Second); agent.count(protocol.TypeBackupRestore) == 0; time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("the agent got no restore")
			}
		}
		agent.last(t, protocol.TypeBackupRestore, &sent)
		agentAnswers(servers, server.ID, agent, protocol.TypeBackupRestore, protocol.TypeOK, nil)
		waitRestore(t, apps, started.ID, RestoreDone)
		return sent, started.To, nil
	}

	// The name typed out is checked here, for every surface.
	for _, typed := range []string{"", "blog", "shop2"} {
		if _, err := apps.StartRestore(ctx, owner, "shop", RestoreTarget{Moment: "1h", Confirm: typed}); !errors.Is(err, ErrConfirm) {
			t.Fatalf("confirm %q: %v", typed, err)
		}
	}
	if agent.count(protocol.TypeBackupRestore) != 0 || agent.count(protocol.TypeBackupList) != 0 {
		t.Fatal("a restore without the name reached the agent")
	}
	sent, to, err := restore(RestoreTarget{Moment: "before v7"})
	if err != nil || sent.Copy != before.Format(protocol.CopyIDLayout) || sent.At != nil || sent.Database != "/data/app.db" || to != "Before v7" {
		t.Fatalf("before v7: %+v %v", sent, err)
	}
	sent, _, err = restore(RestoreTarget{Moment: "15m"})
	if err != nil || sent.At == nil || time.Since(*sent.At) < 14*time.Minute || time.Since(*sent.At) > 16*time.Minute || sent.Copy != "" {
		t.Fatalf("15m: %+v %v", sent, err)
	}
	sent, _, err = restore(RestoreTarget{Moment: "undo-20261107T140200Z"})
	if err != nil || sent.Undo != "20261107T140200Z" || sent.At != nil || sent.Copy != "" {
		t.Fatalf("undo: %+v %v", sent, err)
	}
	at := now.Add(-time.Hour).Truncate(time.Minute)
	sent, _, err = restore(RestoreTarget{At: &at})
	if err != nil || sent.At == nil || !sent.At.Equal(at) {
		t.Fatalf("a minute: %+v %v", sent, err)
	}
	// Before Every change reaches: the newest copy at or before it.
	early := now.Add(-5 * time.Hour)
	if _, _, err := restore(RestoreTarget{At: &early}); !errors.Is(err, ErrMomentOutside) {
		t.Fatalf("before every copy: %v", err)
	}
	backup.Since = nil
	sent, _, err = restore(RestoreTarget{At: &at})
	if err != nil || sent.Copy != copied.Format(protocol.CopyIDLayout) || sent.At != nil {
		t.Fatalf("a time without Every change: %+v %v", sent, err)
	}
	if _, _, err := restore(RestoreTarget{Moment: "before v99"}); !errors.Is(err, ErrMoment) {
		t.Fatalf("a moment that is not there: %v", err)
	}
	if agent.count(protocol.TypeBackupRestore) != 0 {
		t.Error("a refused restore reached the agent")
	}
	if _, _, err := restore(RestoreTarget{Database: "/data/other.db", Moment: "1h"}); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("another database: %v", err)
	}
}

// waitRestore waits until a restore of shop is in state.
func waitRestore(t *testing.T, apps *AppService, id, state string) *Restore {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		restore, err := apps.RestoreOf(owner.WorkspaceID, "shop", id)
		if err != nil {
			t.Fatal(err)
		}
		if restore.State == state {
			return restore
		}
		if time.Now().After(deadline) {
			t.Fatalf("restore %+v, want %s", restore, state)
		}
	}
}

// TestRestoreRunsOnItsOwn: a restore answers at once and runs until the
// agent is done, longer than any request lasts; a second one waits for
// it, a failure says why, and another workspace never sees it.
func TestRestoreRunsOnItsOwn(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	if _, err := apps.Create(owner, "shop", "web-1"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	since := now.Add(-3 * time.Hour)
	backup := protocol.DatabaseBackup{Database: "/data/app.db", Since: &since, Until: &now}
	ctx := context.Background()
	backupAgent(t, apps, servers, server, agent, backup)
	restore, err := apps.StartRestore(ctx, owner, "shop", RestoreTarget{Confirm: "shop", Moment: "1h"})
	if err != nil || restore.State != RestoreRunning || restore.To != "1 hour ago" || restore.Database != "/data/app.db" {
		t.Fatalf("start: %+v %v", restore, err)
	}
	if latest, err := apps.LatestRestore(owner.WorkspaceID, "shop", "/data/app.db"); err != nil || latest == nil || latest.ID != restore.ID {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	if _, err := apps.StartRestore(ctx, owner, "shop", RestoreTarget{Confirm: "shop", Moment: "1h"}); !errors.Is(err, ErrRestoreRunning) {
		t.Fatalf("a second restore while one runs: %v", err)
	}
	if _, err := apps.RestoreOf(other.WorkspaceID, "shop", restore.ID); err == nil {
		t.Fatal("another workspace sees the restore")
	}
	agentAnswers(servers, server.ID, agent, protocol.TypeBackupRestore, protocol.TypeError, protocol.Error{Message: "swap the data: disk full"})
	failed := waitRestore(t, apps, restore.ID, RestoreFailed)
	if failed.Error != "swap the data: disk full" || failed.FinishedAt == nil {
		t.Fatalf("failed: %+v", failed)
	}

	// The agent goes offline in the middle: the restore is unknown, not
	// failed, and blocks another restore of the app until the agent says
	// how it ended.
	agent.mu.Lock()
	agent.sent = nil
	agent.mu.Unlock()
	backupAgent(t, apps, servers, server, agent, backup)
	restore, err = apps.StartRestore(ctx, owner, "shop", RestoreTarget{Confirm: "shop", Moment: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); agent.count(protocol.TypeBackupRestore) == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the agent got no restore")
		}
	}
	// The agent that is back answers every list with what it knows of the
	// restores.
	// It does not answer until the control plane restarted.
	var says sync.Mutex
	answers := false
	knows := protocol.Backups{App: "shop", Restores: []protocol.RestoreState{{ID: restore.ID, Database: "/data/app.db", State: protocol.RestoreRunning}}}
	servers.Connect(server, testHello, "203.0.113.5", func() {}, func(message protocol.Message) error {
		says.Lock()
		answer := answers
		says.Unlock()
		if (message.Type == protocol.TypeBackupRestores || message.Type == protocol.TypeBackupList) && answer {
			says.Lock()
			reply, _ := protocol.NewMessage(protocol.TypeBackupsResult, knows)
			says.Unlock()
			reply.RequestID = message.RequestID
			go servers.Receive(server.ID, reply)
		}
		return nil
	})
	if gone := waitRestore(t, apps, restore.ID, RestoreUnknown); !strings.Contains(gone.Error, "went offline") || gone.FinishedAt != nil {
		t.Fatalf("gone: %+v", gone)
	}
	if _, err := apps.StartRestore(ctx, owner, "shop", RestoreTarget{Confirm: "shop", Moment: "1h"}); !errors.Is(err, ErrRestoreRunning) {
		t.Fatalf("a restore while one is unknown: %v", err)
	}

	// The control plane restarts: the restore is still there, and the
	// agent that is back says it still runs, then that it is done.
	restarted := NewAppService(apps.apps, servers, apps.box)
	if kept, err := restarted.RestoreOf(owner.WorkspaceID, "shop", restore.ID); err != nil || kept.State != RestoreUnknown {
		t.Fatalf("after a restart: %+v %v", kept, err)
	}
	says.Lock()
	answers = true
	says.Unlock()
	restarted.reconcileRestores(server.ID)
	if still, _ := restarted.RestoreOf(owner.WorkspaceID, "shop", restore.ID); still.State != RestoreRunning {
		t.Fatalf("the agent says it runs: %+v", still)
	}
	// The word that it ended is lost: the minute loop asks a running
	// one again too, until it ended.
	finished := time.Now().UTC()
	says.Lock()
	knows = protocol.Backups{App: "shop", Restores: []protocol.RestoreState{{ID: restore.ID, Database: "/data/app.db", State: protocol.RestoreDone, FinishedAt: &finished}}}
	says.Unlock()
	restarted.askUnknownRestores()
	if done := waitRestore(t, restarted, restore.ID, RestoreDone); done.FinishedAt == nil {
		t.Fatalf("done: %+v", done)
	}
	// A late word changes nothing once it ended.
	agentSays(t, servers, server.ID, protocol.TypeBackupStatus, protocol.Backups{App: "shop", Restores: []protocol.RestoreState{{ID: restore.ID, State: protocol.RestoreFailed, Error: "late"}}})
	if done, _ := restarted.RestoreOf(owner.WorkspaceID, "shop", restore.ID); done.State != RestoreDone {
		t.Fatalf("a late word: %+v", done)
	}

	// The agent writes a restore down the moment it arrives: one it knows
	// nothing of never reached it. The minute loop asks, not only a
	// connect.
	unknown := func(id string) {
		t.Helper()
		if err := apps.apps.AddRestore(Restore{ID: id, WorkspaceID: owner.WorkspaceID, AppID: restore.AppID, App: "shop", State: RestoreUnknown, StartedAt: time.Now().UTC()}, time.Now().Add(-keepRestores)); err != nil {
			t.Fatal(err)
		}
	}
	unknown("lost")
	says.Lock()
	knows = protocol.Backups{App: "shop", Restores: []protocol.RestoreState{}}
	says.Unlock()
	restarted.askUnknownRestores()
	if gone, _ := restarted.RestoreOf(owner.WorkspaceID, "shop", "lost"); gone.State != RestoreFailed || !strings.Contains(gone.Error, "never reached the server") {
		t.Fatalf("a restore the agent knows nothing of: %+v", gone)
	}
	// An agent that reports no restores at all cannot say how one ended:
	// never "it never started".
	unknown("old-agent")
	says.Lock()
	knows = protocol.Backups{App: "shop"}
	says.Unlock()
	restarted.askUnknownRestores()
	if gone, _ := restarted.RestoreOf(owner.WorkspaceID, "shop", "old-agent"); gone.State != RestoreFailed || !strings.HasPrefix(gone.Error, RestoreNoWord) || !strings.Contains(gone.Error, "Check the app") {
		t.Fatalf("a restore of an agent that does not report restores: %+v", gone)
	}
}

// TestOneRestoreAtATime: two restores of one app asked at the same moment
// never both start.
func TestOneRestoreAtATime(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	if _, err := apps.Create(owner, "shop", "web-1"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	since := now.Add(-3 * time.Hour)
	backupAgent(t, apps, servers, server, agent, protocol.DatabaseBackup{Database: "/data/app.db", Since: &since, Until: &now})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := apps.StartRestore(context.Background(), owner, "shop", RestoreTarget{Confirm: "shop", Moment: "1h"})
			results <- err
		}()
	}
	first, second := <-results, <-results
	if (first == nil) == (second == nil) || !errors.Is(first, ErrRestoreRunning) && !errors.Is(second, ErrRestoreRunning) {
		t.Fatalf("two restores at once: %v, %v", first, second)
	}
}

// TestUndoLabelsCarryTheDay: two restores at the same time of day on
// different days read apart, and a label two moments share is refused
// with their ids.
func TestUndoLabelsCarryTheDay(t *testing.T) {
	now := time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC)
	since := now.Add(-6 * 24 * time.Hour)
	backup := protocol.DatabaseBackup{Database: "/data/app.db", Since: &since, Until: &now, Undos: []protocol.Copy{
		{ID: "20261107T150400Z", At: time.Date(2026, 11, 7, 15, 4, 0, 0, time.UTC)},
		{ID: "20261106T150400Z", At: time.Date(2026, 11, 6, 15, 4, 0, 0, time.UTC)},
	}, Copies: []protocol.Copy{
		{ID: "20261107T170000Z", At: now.Add(-time.Hour)},
		{ID: "20261107T160000Z", At: now.Add(-2 * time.Hour)},
	}}
	remembered := []model.BackupMoment{
		{ID: 1, Database: "/data/app.db", Kind: model.MomentEdit, Detail: "sql", At: now.Add(-time.Hour)},
		{ID: 2, Database: "/data/app.db", Kind: model.MomentEdit, Detail: "sql", At: now.Add(-2 * time.Hour)},
	}
	moments := Moments(backup, remembered, now)
	labels := map[string]string{}
	for _, moment := range moments {
		labels[moment.ID] = moment.Label
	}
	if labels["undo-20261107T150400Z"] != "Before the restore at 15:04" || labels["undo-20261106T150400Z"] != "Before the restore on Nov 6 at 15:04" {
		t.Fatalf("labels %v", labels)
	}
	if moment, err := findMoment(moments, "before the restore on nov 6 at 15:04"); err != nil || moment.Undo != "20261106T150400Z" {
		t.Fatalf("by the label with its day: %+v %v", moment, err)
	}
	var ambiguous *AmbiguousMomentError
	if _, err := findMoment(moments, "Before your SQL change"); !errors.As(err, &ambiguous) || !strings.Contains(err.Error(), "edit-1 or edit-2") {
		t.Fatalf("a shared label: %v", err)
	}
	if moment, err := findMoment(moments, "edit-2"); err != nil || moment.ID != "edit-2" {
		t.Fatalf("by its id: %+v %v", moment, err)
	}
}

// TestRememberedMoments: a write through moin and a deploy over an older
// release leave the moment before them, by the server's clock.
func TestRememberedMoments(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	app, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := deployUpload(apps)
	if err != nil {
		t.Fatal(err)
	}
	// The first deploy runs over nothing: no moment.
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true, Detected: protocol.Detected{Kind: "go", Service: "web", Port: 8080, Databases: []protocol.Database{{Path: "/data/app.db"}}}})
	written := time.Now().UTC().Add(-time.Minute)
	go agentAnswers(servers, server.ID, agent, protocol.TypeDBQuery, protocol.TypeDBQueryResult, protocol.DBResult{Database: "/data/app.db", Changes: 1, Before: &written})
	if _, err := apps.Query(ctx, owner, "blog", "", "UPDATE orders SET paid = 1", nil, true, 0); err != nil {
		t.Fatalf("query: %v", err)
	}
	moments, err := apps.apps.BackupMoments(app.ID, time.Now().Add(-time.Hour))
	if err != nil || len(moments) != 1 || moments[0].Kind != model.MomentEdit || moments[0].Detail != "sql" || !moments[0].At.Equal(written) || moments[0].Database != "/data/app.db" {
		t.Fatalf("after a write: %+v %v", moments, err)
	}

	deploy := &model.Deploy{AppID: app.ID}
	if err := apps.apps.CreateDeploy(deploy); err != nil {
		t.Fatal(err)
	}
	apps.markUpdate(deploy.ID, "3.4.1")
	started := time.Now().UTC().Add(-10 * time.Second)
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true, Before: &started})
	moments, _ = apps.apps.BackupMoments(app.ID, time.Now().Add(-time.Hour))
	if len(moments) != 2 || moments[0].Kind != model.MomentUpdate || moments[0].Detail != fmt.Sprint(deploy.Number)+" 3.4.1" {
		t.Fatalf("after an update: %+v", moments)
	}
}

// TestOldAgentGetsAnUpdateAnswer: an agent from before backups as one
// history gets no backup request, and people read what to do.
func TestOldAgentGetsAnUpdateAnswer(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	if _, err := apps.Create(owner, "shop", "web-1"); err != nil {
		t.Fatal(err)
	}
	old := testHello
	old.ProtocolVersion = 1
	sent := 0
	servers.Connect(server, old, "203.0.113.5", func() {}, func(message protocol.Message) error {
		if message.Type == protocol.TypeBackupList || message.Type == protocol.TypeBackupRestore {
			sent++
		}
		return nil
	})
	_, _, err := apps.Backups(context.Background(), owner.WorkspaceID, "shop")
	if !errors.Is(err, ErrAgentOutdated) || !strings.Contains(err.Error(), "update it") {
		t.Fatalf("backups from an old agent: %v", err)
	}
	if BackupProblem(err.Error()) != "The agent on the server is too old: update it." {
		t.Errorf("problem %q", BackupProblem(err.Error()))
	}
	if sent != 0 {
		t.Errorf("an old agent got %d backup requests", sent)
	}
}

// TestDownloads: a download is a signed link to the newest copy, or the
// one named, in the app's own bucket; it expires, and an app without a
// copy says so.
func TestDownloads(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	app, err := apps.Create(owner, "shop", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	newest, older := time.Date(2026, 11, 7, 14, 2, 0, 0, time.UTC), time.Date(2026, 11, 7, 13, 2, 0, 0, time.UTC)
	backup := protocol.DatabaseBackup{Database: "/data/app.db", Copies: []protocol.Copy{{ID: "20261107T140200Z", At: newest, Size: 42}, {ID: "20261107T130200Z", At: older, Size: 40}}}
	answer := func(backup protocol.DatabaseBackup) {
		agent.mu.Lock()
		agent.sent = nil
		agent.mu.Unlock()
		backupAgent(t, apps, servers, server, agent, backup)
	}
	answer(backup)
	download, err := apps.Download(ctx, owner, "shop", "", "")
	if err != nil || download.Copy != "20261107T140200Z" || download.Size != 42 || download.Database != "/data/app.db" {
		t.Fatalf("newest: %+v %v", download, err)
	}
	key := owner.WorkspaceID + "/shop/" + app.ID + "/data-app.db/copies/20261107T140200Z.db.gz"
	if !strings.HasPrefix(download.Link, "https://s3.example.com/owner/"+key) || !strings.Contains(download.Link, "X-Amz-Expires=900") {
		t.Fatalf("link %s", download.Link)
	}
	answer(backup)
	if download, err := apps.Download(ctx, owner, "shop", "", "20261107T130200Z"); err != nil || download.Copy != "20261107T130200Z" {
		t.Fatalf("named: %+v %v", download, err)
	}
	answer(protocol.DatabaseBackup{Database: "/data/app.db"})
	if _, err := apps.Download(ctx, owner, "shop", "", ""); !errors.Is(err, ErrNoCopy) {
		t.Fatalf("no copy: %v", err)
	}
	if _, err := apps.Download(ctx, other, "shop", "", ""); err == nil {
		t.Fatal("another workspace downloads the copy")
	}
}

// TestBackupSchedule: the schedule takes only the choices, goes into the
// activity with before and after, and reaches the agent.
func TestBackupSchedule(t *testing.T) {
	apps, _, _, _ := newAppService(t)
	app, err := apps.Create(owner, "shop", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if app.BackupEvery != 60 || app.BackupKeep != 7 || !app.BackupEveryChange {
		t.Fatalf("defaults: %d %d %v", app.BackupEvery, app.BackupKeep, app.BackupEveryChange)
	}
	if _, err := apps.SetBackupSchedule(owner, "shop", protocol.Schedule{Every: 5, Keep: 7}); !errors.Is(err, ErrBackupEvery) {
		t.Fatalf("every 5: %v", err)
	}
	if _, err := apps.SetBackupSchedule(owner, "shop", protocol.Schedule{Every: 15, Keep: 2}); !errors.Is(err, ErrBackupKeep) {
		t.Fatalf("keep 2: %v", err)
	}
	if _, err := apps.SetBackupSchedule(owner, "shop", protocol.Schedule{Every: 15, Keep: 30}); err != nil {
		t.Fatal(err)
	}
	app, _ = apps.Get(owner.WorkspaceID, "shop")
	if app.BackupEvery != 15 || app.BackupKeep != 30 || app.BackupEveryChange {
		t.Fatalf("stored: %d %d %v", app.BackupEvery, app.BackupKeep, app.BackupEveryChange)
	}
	config := apps.backupConfig(owner.WorkspaceID, app.ServerID)
	if got := config.Schedules["shop"]; got != (protocol.Schedule{Every: 15, Keep: 30}) {
		t.Fatalf("to the agent: %+v", got)
	}
}
