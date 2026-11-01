package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/secret"
	"github.com/axadrn/ops/controlplane/internal/testutil"
	"github.com/axadrn/ops/protocol"
)

// owner and other are members of their own workspaces.
var (
	owner = Actor{WorkspaceID: "workspace-owner", UserID: "owner", Via: "web"}
	other = Actor{WorkspaceID: "workspace-other", UserID: "other", Via: "web"}
)

// testUpload is an upload ID and SHA-256 as the CLI sends them.
var testUpload, testSum = "up_" + strings.Repeat("a", 24), strings.Repeat("0", 64)

// deployUpload deploys a source uploaded to the owner's bucket.
func deployUpload(apps *AppService) (*model.Deploy, error) {
	return apps.DeployUpload(context.Background(), owner, "blog", testUpload, testSum)
}

// fakeAgent is an online server whose agent records what it gets.
type fakeAgent struct {
	mu   sync.Mutex
	sent []protocol.Message
}

func (f *fakeAgent) last(t *testing.T, messageType protocol.MessageType, target any) protocol.Message {
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
	t.Fatalf("the agent got no %s", messageType)
	return protocol.Message{}
}

func newAppService(t *testing.T) (*AppService, *ServerService, *model.Server, *fakeAgent) {
	t.Helper()
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "owner"})
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "other"})
	servers := NewServerService(repository.NewServerRepository(database), repository.NewWorkspaceAuditRepository(database), "https://ops.example.com")
	box, err := secret.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	apps := NewAppService(repository.NewAppRepository(database), servers, box)
	// A bucket for uploads and backups, stored without the check.
	err = apps.apps.SetBackupStorage(model.BackupStorage{WorkspaceID: owner.WorkspaceID, Endpoint: "https://s3.example.com", Region: "auto", Bucket: "owner", AccessKey: "key", SecretKey: box.Seal("secret", owner.WorkspaceID+"/backups")})
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
	agent := &fakeAgent{}
	servers.Connect(server, testHello, "203.0.113.5", func() {}, func(message protocol.Message) error {
		agent.mu.Lock()
		agent.sent = append(agent.sent, message)
		agent.mu.Unlock()
		return nil
	})
	return apps, servers, server, agent
}

func agentSays(t *testing.T, servers *ServerService, serverID string, messageType protocol.MessageType, data any) {
	t.Helper()
	message, err := protocol.NewMessage(messageType, data)
	if err != nil {
		t.Fatal(err)
	}
	servers.Receive(serverID, message)
}

func TestDeployRollbackAndEnv(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
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

	err = apps.SetEnv(owner, "blog", map[string]string{"SECRET": "a$b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := apps.SetEnv(owner, "blog", map[string]string{"1BAD": "x"}, nil); !errors.Is(err, ErrEnvKey) {
		t.Fatalf("bad key: err = %v", err)
	}

	// v1: the agent gets the source URL, its sum and the env.
	first, err := deployUpload(apps)
	if err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	agent.last(t, protocol.TypeDeployStart, &start)
	if start.Number != 1 || start.App != "blog" || start.SourceURL == "" || start.SHA256 == "" || start.Env["SECRET"] != "a$b" {
		t.Fatalf("deploy.start = %+v", start)
	}
	if _, err := deployUpload(apps); !errors.Is(err, ErrDeployRunning) {
		t.Fatalf("second deploy while one runs: err = %v", err)
	}

	_, lines, changes, stop, err := apps.Follow(owner.WorkspaceID, "blog", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	agentSays(t, servers, server.ID, protocol.TypeDeployLog, protocol.DeployLog{DeployID: first.ID, Line: "building"})
	<-changes
	if got := lines(); len(got) != 1 || got[0] != "building" {
		t.Fatalf("lines = %v", got)
	}
	// Another server cannot end this deploy.
	agentSays(t, servers, "someone-else", protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: false})
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true, Detected: protocol.Detected{Kind: "go", Service: "web", Port: 8080}})
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
	second, err := deployUpload(apps)
	if err != nil {
		t.Fatal(err)
	}
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: second.ID, Error: "build failed"})
	app, _ = apps.Get(owner.WorkspaceID, app.ID)
	if app.LatestNumber != 2 || app.LatestStatus != model.DeployFailed || app.LiveNumber != 1 {
		t.Fatalf("after a failed deploy: %+v", app)
	}
	if _, err := apps.Rollback(owner, "blog", 0); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("rollback with nothing before the live deploy: err = %v", err)
	}
	if _, err := apps.Rollback(owner, "blog", 2); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("rollback to a failed deploy: err = %v", err)
	}

	// v3 succeeds, then a rollback to v1 starts its release without env.
	third, err := deployUpload(apps)
	if err != nil {
		t.Fatal(err)
	}
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: third.ID, OK: true})
	rollback, err := apps.Rollback(owner, "blog", 0)
	if err != nil {
		t.Fatal(err)
	}
	start = protocol.DeployStart{}
	agent.last(t, protocol.TypeDeployStart, &start)
	if start.Number != 4 || start.FromNumber != 1 || start.SourceURL != "" || start.Env != nil {
		t.Fatalf("rollback deploy.start = %+v", start)
	}
	if rollback.FromNumber != 1 {
		t.Fatalf("rollback = %+v", rollback)
	}
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: rollback.ID, OK: true})

	// A redeploy builds the newest source again, with the env as it is now.
	err = apps.SetEnv(owner, "blog", map[string]string{"SECRET": "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	redeploy, err := apps.Redeploy(owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
	start = protocol.DeployStart{}
	agent.last(t, protocol.TypeDeployStart, &start)
	if start.Number != 5 || start.SourceNumber != third.Number || start.SourceURL != "" || start.Env["SECRET"] != "new" || redeploy.SourceKey != third.SourceKey {
		t.Fatalf("redeploy deploy.start = %+v", start)
	}
}

func TestLogsAndDeleteAskTheAgent(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	_, err := apps.Create(owner, "blog", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	answer := func(messageType protocol.MessageType, data any) {
		var request protocol.Message
		for request.RequestID == "" {
			agent.mu.Lock()
			// The backup config the agent gets on connect is not the request.
			if len(agent.sent) > 0 && agent.sent[len(agent.sent)-1].Type != protocol.TypeBackupConfig {
				request = agent.sent[len(agent.sent)-1]
			}
			agent.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
		message, _ := protocol.NewMessage(messageType, data)
		message.RequestID = request.RequestID
		servers.Receive(server.ID, message)
	}

	go answer(protocol.TypeLogsResult, protocol.LogsResult{Lines: []string{"web-1  | hello"}})
	lines, err := apps.Logs(context.Background(), owner.WorkspaceID, "blog", 10)
	if err != nil || len(lines) != 1 {
		t.Fatalf("logs = %v, %v", lines, err)
	}

	agent.mu.Lock()
	agent.sent = nil
	agent.mu.Unlock()
	go answer(protocol.TypeError, protocol.Error{Message: "docker is not running"})
	err = apps.Delete(context.Background(), owner, "blog")
	var agentErr *AgentError
	if !errors.As(err, &agentErr) || agentErr.Message != "docker is not running" {
		t.Fatalf("delete with an agent error: err = %v", err)
	}
	if _, err := apps.Get(owner.WorkspaceID, "blog"); err != nil {
		t.Fatalf("a failed delete removed the app: %v", err)
	}
	if err := servers.Delete(owner, server.ID); !errors.Is(err, repository.ErrServerHasApps) {
		t.Fatalf("delete a server with apps: err = %v", err)
	}

	agent.mu.Lock()
	agent.sent = nil
	agent.mu.Unlock()
	go answer(protocol.TypeOK, nil)
	err = apps.Delete(context.Background(), owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
}

func TestImageDeploysHooksAndRegistries(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	_, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Deploy(owner, "blog", "not an image"); !errors.Is(err, ErrImage) {
		t.Fatalf("bad image: err = %v", err)
	}
	if err := apps.SetRegistry(owner, "https://ghcr.io", "me", "x"); !errors.Is(err, ErrRegistryHost) {
		t.Fatalf("registry URL as host: err = %v", err)
	}
	err = apps.SetRegistry(owner, "GHCR.io", "me", "ghp_secret")
	if err != nil {
		t.Fatal(err)
	}
	logins, _ := apps.Registries(owner.WorkspaceID)
	if len(logins) != 1 || logins[0].Host != "ghcr.io" {
		t.Fatalf("registries = %+v", logins)
	}

	// The hook deploys an image and hands the agent the matching login.
	url, err := apps.RotateDeployHook(owner, "blog")
	if err != nil || !strings.HasPrefix(url, "https://ops.example.com/webhooks/deploy/dph_") {
		t.Fatalf("hook url = %q, %v", url, err)
	}
	token := url[strings.LastIndex(url, "/")+1:]
	_, deploy, err := apps.DeployHook(token, "ghcr.io/me/blog:abc")
	if err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	agent.last(t, protocol.TypeDeployStart, &start)
	if start.Image != "ghcr.io/me/blog:abc" || start.SourceURL != "" || start.Registry == nil || start.Registry.Password != "ghp_secret" || deploy.Image != start.Image {
		t.Fatalf("deploy.start = %+v", start)
	}
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})

	// A Docker Hub image gets no ghcr.io login; a redeploy pulls the image again.
	deploy, err = apps.Deploy(owner, "blog", "nginx:1.27")
	if err != nil {
		t.Fatal(err)
	}
	start = protocol.DeployStart{}
	agent.last(t, protocol.TypeDeployStart, &start)
	if start.Registry != nil {
		t.Fatalf("docker hub image got a login: %+v", start.Registry)
	}
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})
	redeploy, err := apps.Redeploy(owner, "blog")
	if err != nil || redeploy.Image != "nginx:1.27" {
		t.Fatalf("redeploy = %+v, %v", redeploy, err)
	}

	// A new hook replaces the old one.
	if _, err := apps.RotateDeployHook(owner, "blog"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := apps.DeployHook(token, "nginx:1.27"); !errors.Is(err, ErrDeployHook) {
		t.Fatalf("old hook: err = %v", err)
	}
	if err := apps.RemoveRegistry(owner, "ghcr.io"); err != nil {
		t.Fatal(err)
	}
}

func TestDomainsAndProtectionApplyToTheRunningRelease(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	_, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	// Before the first deploy there is nothing to apply.
	deploy, err := apps.SetDomains(owner, "blog", []string{"Blog.de", "www.blog.de", "blog.de"}, "blog.de")
	if err != nil || deploy != nil {
		t.Fatalf("set domains before a deploy = %v, %v", deploy, err)
	}
	if _, err := apps.SetDomains(owner, "blog", []string{"not a domain"}, ""); !errors.Is(err, ErrDomain) {
		t.Fatalf("bad domain: err = %v", err)
	}
	if _, err := apps.SetDomains(owner, "blog", []string{"blog.de"}, "other.de"); !errors.Is(err, ErrCanonical) {
		t.Fatalf("bad canonical: err = %v", err)
	}
	first, err := deployUpload(apps)
	if err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	agent.last(t, protocol.TypeDeployStart, &start)
	if start.Routing == nil || len(start.Routing.Domains) != 2 || start.Routing.Domains[0] != "blog.de" || start.Routing.Canonical != "blog.de" {
		t.Fatalf("routing = %+v", start.Routing)
	}
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true})

	// With a live release, protection applies at once, without a build.
	if _, err := apps.SetProtection(owner, "blog", "me", "short"); !errors.Is(err, ErrProtect) {
		t.Fatalf("short password: err = %v", err)
	}
	applied, err := apps.SetProtection(owner, "blog", "me", "a long password")
	if err != nil || applied == nil {
		t.Fatalf("set protection = %v, %v", applied, err)
	}
	start = protocol.DeployStart{}
	agent.last(t, protocol.TypeDeployStart, &start)
	if start.SourceURL != "" || start.Image != "" || start.FromNumber != 0 || !strings.HasPrefix(start.Routing.Protect, "me:$2a$") {
		t.Fatalf("apply deploy.start = %+v", start)
	}
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: applied.ID, OK: true})
	checks, err := apps.CheckDomains(context.Background(), owner.WorkspaceID, "blog")
	if err != nil || len(checks) != 2 || checks[0].Address != "203.0.113.5" || checks[0].Points {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	// A redeploy skips the apply and builds the source again.
	redeploy, err := apps.Redeploy(owner, "blog")
	if err != nil || redeploy.SourceKey != first.SourceKey {
		t.Fatalf("redeploy = %+v, %v", redeploy, err)
	}
}
