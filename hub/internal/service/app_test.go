package service

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/axadrn/moin/protocol"
	"github.com/jmoiron/sqlx"
)

// owner and other are members of their own workspaces.
var (
	owner = Actor{WorkspaceID: "workspace-owner", UserID: "owner", Source: Source{Via: ViaWeb}}
	other = Actor{WorkspaceID: "workspace-other", UserID: "other", Source: Source{Via: ViaWeb}}
)

// deploySource deploys a commit of blog's repo as DeployRef does, without
// asking GitHub: the daemon gets the address of its tarball.
func deploySource(apps *AppService) (*model.Deploy, error) {
	app, err := apps.ready(owner.WorkspaceID, "blog")
	if err != nil {
		return nil, err
	}
	deploy := apps.newDeploy(app)
	deploy.SourceKey = "server"
	return apps.start(owner, "app.deployed", app, deploy, protocol.DeployStart{SourceURL: "https://api.github.com/repos/me/blog/tarball/abc123", SourceStripTop: true})
}

// fakeDaemon is an online server whose daemon records what it gets. It
// keeps env values and secrets as a daemon does, and answers them OK, or
// with the error in refuse for their type.
type fakeDaemon struct {
	mu     sync.Mutex
	sent   []protocol.Message
	refuse map[protocol.MessageType]string
}

// keeps answers the requests a daemon answers at once: env values and
// secrets it keeps.
var keeps = []protocol.MessageType{protocol.TypeEnvSet, protocol.TypeSecretSet, protocol.TypeBucketSet}

// take records a message and answers it when the daemon keeps something.
func (f *fakeDaemon) take(servers *ServerService, serverID string, message protocol.Message) {
	f.mu.Lock()
	f.sent = append(f.sent, message)
	refusal := f.refuse[message.Type]
	f.mu.Unlock()
	if !slices.Contains(keeps, message.Type) {
		return
	}
	reply, _ := protocol.NewMessage(protocol.TypeOK, nil)
	if message.Type == protocol.TypeEnvSet {
		var set protocol.EnvSet
		_ = message.Decode(&set)
		f.mu.Lock()
		keys := slices.Sorted(maps.Keys(f.envOf(set.App)))
		f.mu.Unlock()
		reply, _ = protocol.NewMessage(protocol.TypeEnvResult, protocol.AppEnv{Keys: keys, ChangedAt: time.Now().UTC()})
	}
	if refusal != "" {
		reply, _ = protocol.NewMessage(protocol.TypeError, protocol.Error{Message: refusal})
	}
	reply.RequestID = message.RequestID
	servers.Receive(serverID, reply)
}

// env is the env the server keeps for app, from every env.set it took.
func (f *fakeDaemon) env(t *testing.T, app string) map[string]string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.envOf(app)
}

// envOf is env without the lock.
func (f *fakeDaemon) envOf(app string) map[string]string {
	env := map[string]string{}
	for _, message := range f.sent {
		var set protocol.EnvSet
		if message.Type != protocol.TypeEnvSet || message.Decode(&set) != nil || set.App != app || f.refuse[protocol.TypeEnvSet] != "" {
			continue
		}
		maps.Copy(env, set.Set)
		for _, key := range set.Remove {
			delete(env, key)
		}
	}
	return env
}

func (f *fakeDaemon) last(t *testing.T, messageType protocol.MessageType, target any) protocol.Message {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.sent) - 1; index >= 0; index-- {
		if f.sent[index].Type == messageType {
			if target != nil {
				if err := f.sent[index].Decode(target); err != nil {
					t.Fatal(err)
				}
			}
			return f.sent[index]
		}
	}
	t.Fatalf("the daemon got no %s", messageType)
	return protocol.Message{}
}

// count is how many messages of a type the daemon got.
func (f *fakeDaemon) count(messageType protocol.MessageType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, message := range f.sent {
		if message.Type == messageType {
			n++
		}
	}
	return n
}

func newAppService(t *testing.T) (*AppService, *ServerService, *model.Server, *fakeDaemon) {
	t.Helper()
	apps, servers, server, daemon, _ := newAppServiceOn(t)
	return apps, servers, server, daemon
}

// newAppServiceOn is newAppService with the hub's database.
func newAppServiceOn(t *testing.T) (*AppService, *ServerService, *model.Server, *fakeDaemon, *sqlx.DB) {
	t.Helper()
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "owner"})
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "other"})
	servers := NewServerService(repository.NewServers(database), repository.NewActivity(database), "https://moin.example.com")
	box, err := secret.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	apps := NewAppService(repository.NewApps(database, database), servers, box)
	// A bucket for backups, stored without the check; the server keeps
	// its key.
	err = apps.apps.CreateBucket(model.Bucket{ID: "bucket-owner", WorkspaceID: owner.WorkspaceID, Name: "default", IsDefault: true, Endpoint: "https://s3.example.com", Region: "auto", Bucket: "owner", AccessKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	server, installToken, err := servers.Create(owner, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = servers.Register(installToken, testHello)
	if err != nil {
		t.Fatal(err)
	}
	daemon := &fakeDaemon{}
	hello := testHello
	hello.Secrets = []string{protocol.BucketSecret("bucket-owner")}
	servers.Connect(server, hello, "203.0.113.5", func() {}, func(message protocol.Message) error {
		daemon.take(servers, server.ID, message)
		return nil
	})
	return apps, servers, server, daemon, database
}

// connectServer adds a server to the actor's workspace whose daemon
// connects with secrets, the names of the secrets it keeps.
func connectServer(t *testing.T, servers *ServerService, actor Actor, name string, secrets ...string) (*model.Server, *fakeDaemon) {
	t.Helper()
	server, installToken, err := servers.Create(actor, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := servers.Register(installToken, testHello); err != nil {
		t.Fatal(err)
	}
	daemon := &fakeDaemon{}
	hello := testHello
	hello.Secrets = secrets
	servers.Connect(server, hello, "203.0.113.6", func() {}, func(message protocol.Message) error {
		daemon.take(servers, server.ID, message)
		return nil
	})
	return server, daemon
}

func daemonSays(t *testing.T, servers *ServerService, serverID string, messageType protocol.MessageType, data any) {
	t.Helper()
	message, err := protocol.NewMessage(messageType, data)
	if err != nil {
		t.Fatal(err)
	}
	servers.Receive(serverID, message)
}

// daemonAnswers waits for the request of the given type and answers it: what
// the daemon gets on connect may arrive in between.
func daemonAnswers(servers *ServerService, serverID string, daemon *fakeDaemon, requestType, messageType protocol.MessageType, data any) {
	var request protocol.Message
	for request.RequestID == "" {
		daemon.mu.Lock()
		for _, sent := range daemon.sent {
			if sent.Type == requestType {
				request = sent
			}
		}
		daemon.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	message, _ := protocol.NewMessage(messageType, data)
	message.RequestID = request.RequestID
	servers.Receive(serverID, message)
}

func TestDeployRollbackAndEnv(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	_, err := apps.Create(owner, "Blog", "web-1")
	if !errors.Is(err, ErrAppName) {
		t.Fatalf("upper case name: err = %v, want ErrAppName", err)
	}
	app, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(other, "blog", "web-1"); !errors.Is(err, repository.ErrServerNotFound) {
		t.Fatalf("app on another user's server: err = %v", err)
	}

	ctx := context.Background()
	_, err = apps.SetEnv(ctx, owner, "blog", map[string]string{"SECRET": "a$b", "OLD": "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetEnv(ctx, owner, "blog", map[string]string{"1BAD": "x"}, nil); !errors.Is(err, ErrEnvKey) {
		t.Fatalf("bad key: err = %v", err)
	}
	// Only keys the app had count as removed.
	if removed, err := apps.SetEnv(ctx, owner, "blog", nil, []string{"OLD", "NOT_THERE"}); err != nil || !slices.Equal(removed, []string{"OLD"}) {
		t.Fatalf("removed = %v, %v; want OLD", removed, err)
	}
	// The values went to the server; the hub keeps the keys and dates.
	if env := daemon.env(t, "blog"); !maps.Equal(env, map[string]string{"SECRET": "a$b"}) {
		t.Fatalf("the server keeps %v", env)
	}
	if vars, _ := apps.EnvVars(owner.WorkspaceID, "blog"); len(vars) != 1 || vars[0].Key != "SECRET" || vars[0].UpdatedAt.IsZero() {
		t.Fatalf("the hub lists %+v", vars)
	}

	// v1: the daemon gets the source URL; the env is on the server.
	first, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Number != 1 || start.App != "blog" || start.SourceURL == "" {
		t.Fatalf("deploy.start = %+v", start)
	}
	if _, err := deploySource(apps); !errors.Is(err, ErrDeployRunning) {
		t.Fatalf("second deploy while one runs: err = %v", err)
	}

	_, lines, changes, stop, err := apps.Follow(owner.WorkspaceID, "blog", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	daemonSays(t, servers, server.ID, protocol.TypeDeployLog, protocol.DeployLog{DeployID: first.ID, Line: "building"})
	<-changes
	if got := lines(); len(got) != 1 || got[0] != "building" {
		t.Fatalf("lines = %v", got)
	}
	// Another server cannot end this deploy.
	daemonSays(t, servers, "someone-else", protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: false})
	if _, _, err := apps.ComposeFile(owner.WorkspaceID, "blog"); !errors.Is(err, ErrNotLive) {
		t.Fatalf("the compose file before the first deploy: err = %v", err)
	}
	composeFile := "name: blog\nservices:\n  web:\n    build:\n      context: .\n"
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true, Detected: protocol.Detected{Kind: "go", Service: "web", Port: 8080}, ComposeFile: composeFile})
	if _, open := <-changes; open {
		t.Fatal("changes stays open after done")
	}
	finished, err := apps.DeployByNumber(owner.WorkspaceID, "blog", 1)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != model.DeploySucceeded || finished.Log != "building" || Detected(*finished).Kind != "go" {
		t.Fatalf("finished deploy = %+v", finished)
	}

	// v2 fails; v1 stays live.
	second, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: second.ID, Error: "build failed", ComposeFile: "name: failed\n"})
	app, _ = apps.Get(owner.WorkspaceID, app.ID)
	if app.LatestNumber != 2 || app.LatestStatus != model.DeployFailed || app.LiveNumber != 1 {
		t.Fatalf("after a failed deploy: %+v", app)
	}
	// The compose file is the live deploy's, not the failed one's.
	if number, text, err := apps.ComposeFile(owner.WorkspaceID, "blog"); err != nil || number != 1 || text != composeFile {
		t.Fatalf("the compose file = v%d %q, %v", number, text, err)
	}
	if _, err := apps.Rollback(owner, "blog", 0); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("rollback with nothing before the live deploy: err = %v", err)
	}
	if _, err := apps.Rollback(owner, "blog", 2); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("rollback to a failed deploy: err = %v", err)
	}
	if _, err := apps.Rollback(owner, "blog", 9); !errors.Is(err, repository.ErrDeployNotFound) {
		t.Fatalf("rollback to a deploy that does not exist: err = %v", err)
	}

	// v3 succeeds, then a rollback to v1 starts its release, with the env
	// the server keeps now.
	third, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	// A compose file too big to keep is dropped.
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: third.ID, OK: true, ComposeFile: strings.Repeat("x", maxComposeFile+1)})
	if _, _, err := apps.ComposeFile(owner.WorkspaceID, "blog"); !errors.Is(err, ErrNoComposeFile) {
		t.Fatalf("the compose file of a deploy with one too big: err = %v", err)
	}
	rollback, err := apps.Rollback(owner, "blog", 0)
	if err != nil {
		t.Fatal(err)
	}
	start = protocol.DeployStart{}
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Number != 4 || start.FromNumber != 1 || start.SourceURL != "" {
		t.Fatalf("rollback deploy.start = %+v", start)
	}
	if rollback.FromNumber != 1 {
		t.Fatalf("rollback = %+v", rollback)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: rollback.ID, OK: true})
	// A daemon from before compose files sends none.
	if _, _, err := apps.ComposeFile(owner.WorkspaceID, "blog"); !errors.Is(err, ErrNoComposeFile) {
		t.Fatalf("the compose file of a deploy without one: err = %v", err)
	}

	// A redeploy builds the newest source again, with the env as it is now.
	_, err = apps.SetEnv(ctx, owner, "blog", map[string]string{"SECRET": "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if env := daemon.env(t, "blog"); env["SECRET"] != "new" {
		t.Fatalf("the server keeps %v", env)
	}
	redeploy, err := apps.Redeploy(owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
	start = protocol.DeployStart{}
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Number != 5 || start.SourceNumber != third.Number || start.SourceURL != "" || redeploy.SourceKey != third.SourceKey {
		t.Fatalf("redeploy deploy.start = %+v", start)
	}

	// The redeploy fails: the next one builds v3 again, not v5.
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: redeploy.ID, Error: "build failed"})
	redeploy, err = apps.Redeploy(owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
	start = protocol.DeployStart{}
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Number != 6 || start.SourceNumber != third.Number {
		t.Fatalf("redeploy after a failed one: deploy.start = %+v", start)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: redeploy.ID, OK: true})

	// Three more builds make v1 the sixth newest build: its release is gone.
	for range 3 {
		deploy, err := deploySource(apps)
		if err != nil {
			t.Fatal(err)
		}
		daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})
	}
	if _, err := apps.Rollback(owner, "blog", 1); !errors.Is(err, ErrDeployGone) {
		t.Fatalf("rollback out of the window: err = %v", err)
	}
	if _, err := apps.Rollback(owner, "blog", third.Number); err != nil {
		t.Fatalf("rollback to the fifth newest build: %v", err)
	}
}

func TestLogsAndDeleteAskTheDaemon(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	_, err := apps.Create(owner, "blog", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	answer := func(requestType, messageType protocol.MessageType, data any) {
		daemonAnswers(servers, server.ID, daemon, requestType, messageType, data)
	}

	go answer(protocol.TypeLogs, protocol.TypeLogsResult, protocol.LogsResult{Lines: []string{"web-1  | hello"}})
	lines, err := apps.Logs(context.Background(), owner.WorkspaceID, "blog", 10)
	if err != nil || len(lines) != 1 {
		t.Fatalf("logs = %v, %v", lines, err)
	}

	daemon.mu.Lock()
	daemon.sent = nil
	daemon.mu.Unlock()
	go answer(protocol.TypeAppRemove, protocol.TypeError, protocol.Error{Message: "docker is not running"})
	err = apps.Delete(context.Background(), owner, "blog", "blog")
	var daemonErr *DaemonError
	if !errors.As(err, &daemonErr) || daemonErr.Message != "docker is not running" {
		t.Fatalf("delete with a daemon error: err = %v", err)
	}
	if _, err := apps.Get(owner.WorkspaceID, "blog"); err != nil {
		t.Fatalf("a failed delete removed the app: %v", err)
	}
	// It is online: its apps go first, so nothing runs on unseen.
	if _, _, err := servers.Delete(owner, server.ID, server.Name); !errors.Is(err, repository.ErrServerHasApps) {
		t.Fatalf("delete an online server with apps: err = %v", err)
	}

	daemon.mu.Lock()
	daemon.sent = nil
	daemon.mu.Unlock()
	go answer(protocol.TypeAppRemove, protocol.TypeOK, nil)
	err = apps.Delete(context.Background(), owner, "blog", "blog")
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteOnAServerThatNeverConnected(t *testing.T) {
	apps, servers, _, _ := newAppService(t)
	server, _, err := servers.Create(owner, "web-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(owner, "blog", server.ID); err != nil {
		t.Fatal(err)
	}
	for _, confirm := range []string{"", "shop"} {
		if err := apps.Delete(context.Background(), owner, "blog", confirm); !errors.Is(err, ErrConfirmMismatch) {
			t.Fatalf("delete with confirm %q: err = %v", confirm, err)
		}
		if _, _, err := servers.Delete(owner, server.ID, confirm); !errors.Is(err, ErrConfirmMismatch) {
			t.Fatalf("delete the server with confirm %q: err = %v", confirm, err)
		}
	}
	if _, err := apps.Get(owner.WorkspaceID, "blog"); err != nil {
		t.Fatalf("a refused confirm removed the app: %v", err)
	}
	if err := apps.Delete(context.Background(), owner, "blog", "blog"); err != nil {
		t.Fatalf("delete on a server that never connected: %v", err)
	}
	// No daemon ever connected, so none is left to uninstall.
	if uninstall, gone, err := servers.Delete(owner, server.ID, server.Name); err != nil || uninstall != "" || len(gone) != 0 {
		t.Fatalf("delete the empty server = %q, %v, %v", uninstall, gone, err)
	}
}

// An offline server can't remove its apps, so they go with it.
func TestDeleteAnOfflineServerTakesItsApps(t *testing.T) {
	apps, servers, _, _ := newAppService(t)
	server, _, err := servers.Create(owner, "dead-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shop", "blog"} {
		if _, err := apps.Create(owner, name, server.ID); err != nil {
			t.Fatal(err)
		}
	}
	_, gone, err := servers.Delete(owner, server.ID, server.Name)
	if err != nil || strings.Join(gone, ",") != "blog,shop" {
		t.Fatalf("delete the offline server = %v, %v", gone, err)
	}
	for _, name := range []string{"shop", "blog"} {
		if _, err := apps.Get(owner.WorkspaceID, name); !errors.Is(err, repository.ErrAppNotFound) {
			t.Errorf("%s is still there: %v", name, err)
		}
	}
	events, err := servers.activity.ListByWorkspace(owner.WorkspaceID, 10)
	if err != nil {
		t.Fatal(err)
	}
	deleted := 0
	for _, event := range events {
		if event.Action == "app.deleted" {
			deleted++
		}
	}
	if deleted != 2 {
		t.Errorf("the activity names %d of the 2 apps", deleted)
	}
}

func TestImageDeploysHooksAndRegistries(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	_, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Deploy(context.Background(), owner, "blog", "not an image", ""); !errors.Is(err, ErrImage) {
		t.Fatalf("bad image: err = %v", err)
	}
	ctx := context.Background()
	if err := apps.SetRegistry(ctx, owner, "https://ghcr.io", "me", "x", ""); !errors.Is(err, ErrRegistryHost) {
		t.Fatalf("registry URL as host: err = %v", err)
	}
	err = apps.SetRegistry(ctx, owner, "GHCR.io", "me", "ghp_secret", "")
	if err != nil {
		t.Fatal(err)
	}
	logins, _ := apps.Registries(owner.WorkspaceID)
	if len(logins) != 1 || logins[0].Host != "ghcr.io" || len(logins[0].Servers.On) != 1 || logins[0].Servers.On[0].Server != "web-1" {
		t.Fatalf("registries = %+v", logins)
	}
	// The login went to the server, never into a deploy.
	var secret protocol.SecretSet
	daemon.last(t, protocol.TypeSecretSet, &secret)
	if secret != (protocol.SecretSet{Name: "registry/ghcr.io", Value: "me:ghp_secret"}) {
		t.Fatalf("secret.set = %+v", secret)
	}

	// A hook deploys nothing while no image runs: a person picks the image.
	url, err := apps.RotateDeployHook(owner, "blog")
	if err != nil || !strings.HasPrefix(url, "https://moin.example.com/webhooks/deploy/dph_") {
		t.Fatalf("hook url = %q, %v", url, err)
	}
	token := url[strings.LastIndex(url, "/")+1:]
	var refused *HookImageError
	if _, _, err := apps.DeployHook(context.Background(), token, "ghcr.io/me/blog:abc", ""); !errors.As(err, &refused) || refused.Image != "" {
		t.Fatalf("hook before an image runs: err = %v", err)
	}
	// An app without a repo keeps the compose file it runs: a commit
	// means nothing there.
	if _, err := apps.Deploy(context.Background(), owner, "blog", "ghcr.io/me/blog:v1", "main"); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("a ref without a repo: err = %v", err)
	}
	if _, err := apps.Deploy(context.Background(), owner, "blog", "ghcr.io/me/blog:v1", "--upload-pack=x"); !errors.Is(err, ErrRefName) {
		t.Fatalf("a ref that is no name: err = %v", err)
	}
	first, err := apps.Deploy(context.Background(), owner, "blog", "ghcr.io/me/blog:v1", "")
	if err != nil {
		t.Fatal(err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true})

	// Then the hook deploys a new tag; the server pulls it with its login.
	_, deploy, err := apps.DeployHook(context.Background(), token, "ghcr.io/me/blog:abc", "")
	if err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Image != "ghcr.io/me/blog:abc" || start.SourceURL != "" || deploy.Image != start.Image {
		t.Fatalf("deploy.start = %+v", start)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})
	if _, _, err := apps.DeployHook(context.Background(), token, "ghcr.io/me/blog:abc", "abc123"); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("hook with a commit without a repo: err = %v", err)
	}

	// A restart runs the same image, so the hook keeps deploying its new
	// tags. A rollback, to the restart too, runs the image of then.
	restart, err := apps.Restart(owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: restart.ID, OK: true})
	_, deploy, err = apps.DeployHook(context.Background(), token, "ghcr.io/me/blog:def", "")
	if err != nil {
		t.Fatalf("hook after a restart: %v", err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})
	back, err := apps.Rollback(owner, "blog", restart.Number)
	if err != nil || back.Image != "ghcr.io/me/blog:abc" {
		t.Fatalf("rollback to the restart = %+v, %v", back, err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: back.ID, OK: true})
	if app, _ := apps.Get(owner.WorkspaceID, "blog"); app.LiveImage != "ghcr.io/me/blog:abc" {
		t.Fatalf("after the rollback the image is %q", app.LiveImage)
	}
	if _, deploy, err = apps.DeployHook(context.Background(), token, "ghcr.io/me/blog:ghi", ""); err != nil {
		t.Fatalf("hook after a rollback: %v", err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})

	// A leaked hook URL cannot start another image, not even one with the
	// same name on another host.
	for _, other := range []string{"nginx:1.27", "ghcr.io/evil/blog:abc", "evil.example.com/me/blog:abc"} {
		if _, _, err := apps.DeployHook(context.Background(), token, other, ""); !errors.As(err, &refused) || refused.Image != "ghcr.io/me/blog" {
			t.Fatalf("hook deployed %s: err = %v", other, err)
		}
	}

	// A Docker Hub image needs no login; a redeploy pulls the image again.
	deploy, err = apps.Deploy(context.Background(), owner, "blog", "nginx:1.27", "")
	if err != nil {
		t.Fatal(err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})
	redeploy, err := apps.Redeploy(owner, "blog")
	if err != nil || redeploy.Image != "nginx:1.27" {
		t.Fatalf("redeploy = %+v, %v", redeploy, err)
	}

	// A person changed the image by hand, so the hook follows it.
	if _, _, err := apps.DeployHook(context.Background(), token, "ghcr.io/me/blog:abc", ""); !errors.As(err, &refused) || refused.Image != "docker.io/library/nginx" {
		t.Fatalf("hook after nginx by hand: err = %v", err)
	}

	// A new hook replaces the old one.
	if _, err := apps.RotateDeployHook(owner, "blog"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := apps.DeployHook(context.Background(), token, "nginx:1.27", ""); !errors.Is(err, ErrDeployHook) {
		t.Fatalf("old hook: err = %v", err)
	}
	if err := apps.RemoveRegistry(ctx, owner, "ghcr.io"); err != nil {
		t.Fatal(err)
	}
	secret = protocol.SecretSet{}
	daemon.last(t, protocol.TypeSecretSet, &secret)
	if secret != (protocol.SecretSet{Name: "registry/ghcr.io"}) {
		t.Fatalf("after remove, secret.set = %+v", secret)
	}
}

func TestDomainsAndProtectionWaitForARestart(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	_, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := apps.SetDomains(owner, "blog", "", []string{"Blog.example.com", "www.blog.example.com", "blog.example.com"}, "blog.example.com")
	if err != nil || !changed {
		t.Fatalf("set domains before a deploy = %v, %v", changed, err)
	}
	if changed, err := apps.RemoveProtection(owner, "blog"); err != nil || !changed {
		t.Fatalf("the password of a new app, turned off = %v, %v", changed, err)
	}
	if changed, err := apps.RemoveProtection(owner, "blog"); err != nil || changed {
		t.Fatalf("no password, turned off = %v, %v", changed, err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"not a domain"}, ""); !errors.Is(err, ErrDomain) {
		t.Fatalf("bad domain: err = %v", err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, "other.example.org"); !errors.Is(err, ErrCanonical) {
		t.Fatalf("bad canonical: err = %v", err)
	}
	first, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Domains == nil || len(start.Domains.Domains) != 2 || start.Domains.Domains[0] != "blog.example.com" || start.Domains.Canonical != "blog.example.com" || start.Domains.Protect != "" {
		t.Fatalf("domains = %+v", start.Domains)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true})
	waiting := func() int {
		t.Helper()
		app, err := apps.Get(owner.WorkspaceID, "blog")
		if err != nil {
			t.Fatal(err)
		}
		n, err := apps.ChangesWaiting(owner.WorkspaceID, app)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := waiting(); n != 0 {
		t.Fatalf("changes waiting after the deploy = %d", n)
	}

	// With a live deploy, saving restarts nothing: the changes wait.
	if _, _, err := apps.SetProtection(owner, "blog", "me", "short"); !errors.Is(err, ErrProtect) {
		t.Fatalf("short password: err = %v", err)
	}
	sent := daemon.count(protocol.TypeDeployStart)
	if _, changed, err := apps.SetProtection(owner, "blog", "me", "a long password"); err != nil || !changed {
		t.Fatalf("set protection = %v, %v", changed, err)
	}
	if changed, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil || !changed {
		t.Fatalf("set domains = %v, %v", changed, err)
	}
	if daemon.count(protocol.TypeDeployStart) != sent {
		t.Fatal("saving started a deploy")
	}
	if n := waiting(); n != 2 {
		t.Fatalf("changes waiting = %d, want 2", n)
	}
	// The same password or the same domains change nothing.
	if _, changed, err := apps.SetProtection(owner, "blog", "me", "a long password"); err != nil || changed {
		t.Fatalf("the same password = %v, %v", changed, err)
	}
	if changed, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil || changed {
		t.Fatalf("the same domains = %v, %v", changed, err)
	}

	// Restart applies them, without a build.
	restarted, err := apps.Restart(owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
	start = protocol.DeployStart{}
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.SourceURL != "" || start.Image != "" || !strings.HasPrefix(start.Domains.Protect, "me:$2a$") || len(start.Domains.Domains) != 1 {
		t.Fatalf("restart deploy.start = %+v", start)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: restarted.ID, OK: true})
	if n := waiting(); n != 0 {
		t.Fatalf("changes waiting after the restart = %d", n)
	}
	checks, err := apps.CheckDomains(context.Background(), owner.WorkspaceID, "blog")
	if err != nil || len(checks) != 1 || checks[0].Address != "203.0.113.5" || checks[0].Points || !checks[0].Checked {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	// The web lists them without a lookup, and checks only the one asked.
	listed, err := apps.Domains(owner.WorkspaceID, "blog")
	if err != nil || len(listed) != 1 || listed[0].Checked || listed[0].Advice != "A record to 203.0.113.5" {
		t.Fatalf("domains = %+v, %v", listed, err)
	}
	if checks, err := apps.CheckDomain(context.Background(), owner.WorkspaceID, "blog", "other.example.org"); err != nil || checks[0].Checked {
		t.Fatalf("check of another domain = %+v, %v", checks, err)
	}
	if checks, err := apps.CheckDomain(context.Background(), owner.WorkspaceID, "blog", "blog.example.com"); err != nil || !checks[0].Checked {
		t.Fatalf("check of blog.example.com = %+v, %v", checks, err)
	}
	// A redeploy builds the source again.
	redeploy, err := apps.Redeploy(owner, "blog")
	if err != nil || redeploy.SourceKey != first.SourceKey {
		t.Fatalf("redeploy = %+v, %v", redeploy, err)
	}
}

// Domains come only from moin: after a deploy, moin lists the ones saved
// in it and changes them.
func TestDomainsChangeAfterADeploy(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	first, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	detected := protocol.Detected{Kind: "compose", Service: "web", Domains: []string{"blog.example.com"}}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true, Detected: detected})
	if _, err := apps.SetDomains(owner, "blog", "", []string{"shop.example.com", "www.shop.example.com"}, "shop.example.com"); err != nil {
		t.Fatalf("set domains after a deploy: %v", err)
	}
	if _, err := apps.SetDomains(owner, "blog", "api", []string{"api.shop.example.com"}, ""); err != nil {
		t.Fatalf("set domains of another service after a deploy: %v", err)
	}
	checks, err := apps.CheckDomains(context.Background(), owner.WorkspaceID, "blog")
	if err != nil || len(checks) != 3 || checks[0].Domain != "shop.example.com" || !checks[0].Canonical || checks[2].Service != "api" {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
}

func TestServerAddressesAndAdvice(t *testing.T) {
	// A daemon that connects over IPv6 and reports its IPv4 too.
	server := model.Server{Address: "2a01:4f8::1", Addresses: "203.0.113.7 2a01:4f8::1"}
	ipv4, ipv6 := ServerAddresses(server)
	if ipv4 != "203.0.113.7" || ipv6 != "2a01:4f8::1" {
		t.Fatalf("addresses = %q, %q", ipv4, ipv6)
	}
	if advice := DNSAdvice(ipv4, ipv6); advice != "A record to 203.0.113.7, AAAA record to 2a01:4f8::1" {
		t.Fatalf("advice = %q", advice)
	}
	if advice := DNSAdvice("", ipv6); advice != "AAAA record to 2a01:4f8::1" {
		t.Fatalf("advice without IPv4 = %q", advice)
	}
	// A daemon next to the hub connects from loopback or a
	// private network; only its reported public address counts.
	for _, address := range []string{"::1", "172.17.0.1"} {
		ipv4, ipv6 := ServerAddresses(model.Server{Address: address, Addresses: "203.0.113.7"})
		if ipv4 != "203.0.113.7" || ipv6 != "" {
			t.Fatalf("addresses from %s = %q, %q", address, ipv4, ipv6)
		}
	}
	if !resolvesTo([]string{"203.0.113.7"}, ipv4, ipv6) || !resolvesTo([]string{"2a01:04f8:0::1"}, ipv4, ipv6) || resolvesTo([]string{"198.51.100.1"}, ipv4, ipv6) {
		t.Fatal("resolvesTo does not accept either address")
	}
}

func TestStopStartAndRestart(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	_, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	// Before the first deploy there is nothing to stop or restart.
	if err := apps.Stop(context.Background(), owner, "blog"); !errors.Is(err, ErrNotLive) {
		t.Fatalf("stop before a deploy: err = %v", err)
	}
	if _, err := apps.Restart(owner, "blog"); !errors.Is(err, ErrNotLive) {
		t.Fatalf("restart before a deploy: err = %v", err)
	}
	first, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true})

	// A running app does not start again.
	daemon.mu.Lock()
	daemon.sent = nil
	daemon.mu.Unlock()
	if _, err := apps.Start(owner, "blog"); !errors.Is(err, ErrRunning) {
		t.Fatalf("start a running app: err = %v", err)
	}
	daemon.mu.Lock()
	sent := len(daemon.sent)
	daemon.mu.Unlock()
	if _, deploys, _ := apps.Deploys(owner.WorkspaceID, "blog", 10); len(deploys) != 1 || sent != 0 {
		t.Fatalf("start of a running app made %d deploys, sent %d messages", len(deploys), sent)
	}

	// A failed stop leaves the app as it was.
	go daemonAnswers(servers, server.ID, daemon, protocol.TypeAppStop, protocol.TypeError, protocol.Error{Message: "docker is not running"})
	var daemonErr *DaemonError
	if err := apps.Stop(context.Background(), owner, "blog"); !errors.As(err, &daemonErr) {
		t.Fatalf("stop with a daemon error: err = %v", err)
	}
	if app, _ := apps.Get(owner.WorkspaceID, "blog"); app.Stopped {
		t.Fatal("a failed stop marked the app stopped")
	}

	daemon.mu.Lock()
	daemon.sent = nil
	daemon.mu.Unlock()
	go daemonAnswers(servers, server.ID, daemon, protocol.TypeAppStop, protocol.TypeOK, nil)
	if err := apps.Stop(context.Background(), owner, "blog"); err != nil {
		t.Fatal(err)
	}
	var stop protocol.AppStop
	daemon.last(t, protocol.TypeAppStop, &stop)
	if stop.App != "blog" {
		t.Fatalf("app.stop = %+v", stop)
	}
	if app, _ := apps.Get(owner.WorkspaceID, "blog"); !app.Stopped {
		t.Fatal("the app is not marked stopped")
	}

	// A stopped app keeps a domain change for its start.
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil {
		t.Fatalf("set domains of a stopped app: %v", err)
	}

	// Start runs the live deploy with the settings as they are now.
	started, err := apps.Start(owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.SourceURL != "" || start.Image != "" || start.FromNumber != 0 || len(start.Domains.Domains) != 1 {
		t.Fatalf("start deploy.start = %+v", start)
	}
	if app, _ := apps.Get(owner.WorkspaceID, "blog"); app.Stopped {
		t.Fatal("a started app is still marked stopped")
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: started.ID, OK: true})

	restarted, err := apps.Restart(owner, "blog")
	if err != nil || restarted.Number != started.Number+1 {
		t.Fatalf("restart = %+v, %v", restarted, err)
	}
}
