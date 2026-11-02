package service

import (
	"errors"
	"github.com/axadrn/ops/controlplane/internal/access"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/testutil"
	"github.com/axadrn/ops/protocol"
)

func newServerService(t *testing.T) *ServerService {
	t.Helper()
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "owner"})
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "other"})
	return NewServerService(repository.NewServerRepository(database), repository.NewWorkspaceAuditRepository(database), "https://ops.example.com/")
}

var testHello = protocol.Hello{ProtocolVersion: protocol.Version, AgentVersion: "v0.1.0", Hostname: "box", OS: "linux", Arch: "arm64"}

func TestInstallTokenWorksOnce(t *testing.T) {
	servers := newServerService(t)
	server, installToken, err := servers.Create(owner, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(installToken, "ins_") {
		t.Fatalf("install token %q has no marker", installToken)
	}
	if got := servers.InstallCommand(installToken); got != "curl -fsSL https://ops.example.com/install.sh | sudo sh -s -- "+installToken {
		t.Fatalf("install command = %q", got)
	}

	registered, agentToken, err := servers.Register(installToken, testHello)
	if err != nil {
		t.Fatal(err)
	}
	if registered.ID != server.ID {
		t.Fatalf("registered %s, want %s", registered.ID, server.ID)
	}
	_, _, err = servers.Register(installToken, testHello)
	if !errors.Is(err, ErrInstallTokenValid) {
		t.Fatalf("second register: err = %v, want ErrInstallTokenValid", err)
	}

	authenticated, err := servers.Authenticate(agentToken)
	if err != nil || authenticated.ID != server.ID {
		t.Fatalf("authenticate = %v, %v", authenticated, err)
	}
	if authenticated.OS != "linux" || authenticated.Arch != "arm64" || authenticated.Hostname != "box" {
		t.Fatalf("register did not store the hello: %+v", authenticated)
	}
	_, err = servers.Authenticate(installToken)
	if !errors.Is(err, ErrAgentToken) {
		t.Fatalf("install token as agent token: err = %v", err)
	}
}

func TestInstallTokenExpires(t *testing.T) {
	servers := newServerService(t)
	_, installToken, err := servers.Create(owner, "prod")
	if err != nil {
		t.Fatal(err)
	}
	servers.now = func() time.Time { return time.Now().Add(InstallTokenLifetime + time.Minute) }
	_, _, err = servers.Register(installToken, testHello)
	if !errors.Is(err, ErrInstallTokenValid) {
		t.Fatalf("err = %v, want ErrInstallTokenValid", err)
	}
}

func TestServerNames(t *testing.T) {
	servers := newServerService(t)
	_, _, err := servers.Create(owner, "prod")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = servers.Create(owner, " prod ")
	if !errors.Is(err, repository.ErrDuplicateServer) {
		t.Fatalf("duplicate name: err = %v", err)
	}
	_, _, err = servers.Create(other, "prod")
	if err != nil {
		t.Fatalf("another user may use the same name: %v", err)
	}
	_, _, err = servers.Create(owner, strings.Repeat("x", 65))
	if !errors.Is(err, ErrServerName) {
		t.Fatalf("long name: err = %v", err)
	}
	generatedName := regexp.MustCompile(`^[a-z]+-[a-z]+$`)
	first, _, err := servers.Create(owner, "")
	if err != nil || !generatedName.MatchString(first.Name) {
		t.Fatalf("empty name = %v, %v", first, err)
	}
	second, _, err := servers.Create(owner, " ")
	if err != nil || !generatedName.MatchString(second.Name) || second.Name == first.Name {
		t.Fatalf("second empty name = %v, %v", second, err)
	}
}

func TestSessionsMarkServersOnline(t *testing.T) {
	servers := newServerService(t)
	server, installToken, _ := servers.Create(owner, "prod")
	_, agentToken, _ := servers.Register(installToken, testHello)
	authenticated, _ := servers.Authenticate(agentToken)

	changes, unsubscribe := servers.Subscribe(owner.WorkspaceID)
	defer unsubscribe()

	kicked := 0
	disconnect := servers.Connect(authenticated, testHello, "203.0.113.5", func() { kicked++ }, func(protocol.Message) error { return nil })
	expectChange(t, changes)
	list, _ := servers.List(owner.WorkspaceID)
	if len(list) != 1 || !list[0].Online || list[0].LastSeenAt == nil {
		t.Fatalf("after connect: %+v", list)
	}

	// A second session of the same server replaces the first.
	disconnectSecond := servers.Connect(authenticated, testHello, "203.0.113.5", func() { kicked++ }, func(protocol.Message) error { return nil })
	if kicked != 1 {
		t.Fatalf("kicked = %d, want the first session kicked", kicked)
	}
	disconnect()
	list, _ = servers.List(owner.WorkspaceID)
	if !list[0].Online {
		t.Fatal("the old session's disconnect took the new session offline")
	}
	disconnectSecond()
	list, _ = servers.List(owner.WorkspaceID)
	if list[0].Online {
		t.Fatal("still online after the session ended")
	}

	foreign, _ := servers.List(other.WorkspaceID)
	if len(foreign) != 0 {
		t.Fatalf("another workspace sees %d servers", len(foreign))
	}

	servers.Connect(authenticated, testHello, "203.0.113.5", func() { kicked++ }, func(protocol.Message) error { return nil })
	err := servers.Delete(other, server.ID)
	if !errors.Is(err, repository.ErrServerNotFound) {
		t.Fatalf("another user deleted the server: %v", err)
	}
	err = servers.Delete(owner, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if kicked != 2 {
		t.Fatalf("kicked = %d, want delete to end the session", kicked)
	}
	_, err = servers.Authenticate(agentToken)
	if !errors.Is(err, ErrAgentToken) {
		t.Fatalf("agent token after delete: err = %v", err)
	}
}

func expectChange(t *testing.T, changes <-chan struct{}) {
	t.Helper()
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("no change notification")
	}
}

func TestAddingServersNeedsThePlan(t *testing.T) {
	servers := newServerService(t)
	plan := access.PlanFree
	servers.RequirePlanFor(func(string) (access.Access, error) {
		return access.Resolve(access.Subscription{PlanID: plan, Status: access.StatusActive}, time.Now()), nil
	})
	if servers.CanAdd(owner.WorkspaceID) {
		t.Fatal("the free plan may add a server")
	}
	_, _, err := servers.Create(owner, "web-1")
	if !errors.Is(err, ErrSubscriptionRequired) {
		t.Fatalf("free plan: err = %v", err)
	}
	plan = access.PlanPro
	_, _, err = servers.Create(owner, "web-1")
	if err != nil || !servers.CanAdd(owner.WorkspaceID) {
		t.Fatalf("ops plan: err = %v", err)
	}
}

func TestRenameServer(t *testing.T) {
	servers := newServerService(t)
	if _, _, err := servers.Create(owner, "prod"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := servers.Create(owner, "staging"); err != nil {
		t.Fatal(err)
	}
	renamed, err := servers.Rename(owner, "prod", " live ")
	if err != nil || renamed.Name != "live" {
		t.Fatalf("rename = %v, %v", renamed, err)
	}
	if _, err := servers.Rename(owner, "live", "staging"); !errors.Is(err, repository.ErrDuplicateServer) {
		t.Fatalf("taken name: err = %v", err)
	}
	if _, err := servers.Rename(owner, "live", " "); !errors.Is(err, ErrServerName) {
		t.Fatalf("empty name: err = %v", err)
	}
	if _, err := servers.Rename(other, "live", "mine"); !errors.Is(err, repository.ErrServerNotFound) {
		t.Fatalf("another workspace: err = %v", err)
	}
}

func TestNewInstallTokenMovesTheAgent(t *testing.T) {
	servers := newServerService(t)
	_, first, err := servers.Create(owner, "prod")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := servers.NewInstallToken(owner, "prod")
	if err != nil || second == first {
		t.Fatalf("new install token = %q, %v", second, err)
	}
	if _, _, err := servers.Register(first, testHello); !errors.Is(err, ErrInstallTokenValid) {
		t.Fatalf("old install token: err = %v", err)
	}
	_, oldAgent, err := servers.Register(second, testHello)
	if err != nil {
		t.Fatal(err)
	}
	_, third, err := servers.NewInstallToken(owner, "prod")
	if err != nil {
		t.Fatal(err)
	}
	_, newAgent, err := servers.Register(third, testHello)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := servers.Authenticate(oldAgent); !errors.Is(err, ErrAgentToken) {
		t.Fatalf("old agent token: err = %v", err)
	}
	if _, err := servers.Authenticate(newAgent); err != nil {
		t.Fatalf("new agent token: %v", err)
	}
	if _, _, err := servers.NewInstallToken(other, "prod"); !errors.Is(err, repository.ErrServerNotFound) {
		t.Fatalf("another workspace: err = %v", err)
	}
}
