package service

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// connect makes a connection of Ada with rights and its first tokens.
func (e deviceLoginEnvironment) connect(t *testing.T, rights model.Rights) Tokens {
	t.Helper()
	connection, err := e.connections.Create(NewConnection{UserID: e.user.User.ID, WorkspaceID: e.user.Workspace.ID, ClientName: "moin CLI on host-1", Rights: rights, Source: Source{Via: ViaWeb}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	tokens, err := e.connections.Issue(connection)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return tokens
}

// TestRefreshRotatesAndAReuseEndsTheConnection: every refresh gives a new
// pair and uses up the refresh token. Showing a used one again ends the
// whole connection: its newest tokens stop too.
func TestRefreshRotatesAndAReuseEndsTheConnection(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	first := e.connect(t, model.DefaultRights)

	second, err := e.connections.Refresh(first.Refresh, "")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if second.Access == first.Access || second.Refresh == first.Refresh || second.Connection.ID != first.Connection.ID {
		t.Fatalf("refresh gave the same tokens or another connection: %+v", second)
	}
	// The old access token keeps its hour, for requests on their way.
	if _, err := e.connections.Authenticate(first.Access); err != nil {
		t.Fatalf("the old access token stopped at once: %v", err)
	}
	third, err := e.connections.Refresh(second.Refresh, "")
	if err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}

	if _, err := e.connections.Refresh(first.Refresh, ""); !errors.Is(err, ErrRefreshReused) {
		t.Fatalf("Refresh() with a used token = %v, want ErrRefreshReused", err)
	}
	for name, raw := range map[string]string{"newest access": third.Access, "old access": first.Access} {
		if _, err := e.connections.Authenticate(raw); err == nil {
			t.Errorf("%s token works after the reuse", name)
		}
	}
	if _, err := e.connections.Refresh(third.Refresh, ""); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("Refresh() with the newest token after the reuse = %v, want invalid", err)
	}
	connections, err := e.connections.List(e.user.User.ID)
	if err != nil || len(connections) != 0 {
		t.Fatalf("the connection is still listed: %+v, %v", connections, err)
	}
	if !e.recorded(t, "connection.ended", "the refresh token was used twice") {
		t.Fatal("the activity does not say why the connection ended")
	}
}

// TestTwoRefreshesAtOnceEndTheConnection: only one exchange of a refresh
// token wins; the second is a reuse.
func TestTwoRefreshesAtOnceEndTheConnection(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	first := e.connect(t, model.DefaultRights)
	token, _, err := e.connections.connections.Token(hashToken(first.Refresh), model.ConnectionTokenRefresh)
	if err != nil {
		t.Fatal(err)
	}
	// Another exchange used it between the read and the write.
	if err := e.connections.connections.UseRefresh(token.ID, e.clock.now); err != nil {
		t.Fatal(err)
	}
	if err := e.connections.connections.UseRefresh(token.ID, e.clock.now); !errors.Is(err, repository.ErrRefreshTokenUsed) {
		t.Fatalf("second UseRefresh() = %v, want ErrRefreshTokenUsed", err)
	}
}

// TestAccessTokensLastAnHour and refresh tokens are of no use to call the
// API, nor access tokens to refresh.
func TestAccessTokensLastAnHour(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	tokens := e.connect(t, model.DefaultRights)
	if _, err := e.connections.Authenticate(tokens.Refresh); err == nil {
		t.Fatal("a refresh token calls the API")
	}
	if _, err := e.connections.Refresh(tokens.Access, ""); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("Refresh() with an access token = %v, want invalid", err)
	}
	e.clock.Add(model.AccessTokenLifetime - time.Second)
	if _, err := e.connections.Authenticate(tokens.Access); err != nil {
		t.Fatalf("the access token stopped before its hour: %v", err)
	}
	e.clock.Add(time.Second)
	if _, err := e.connections.Authenticate(tokens.Access); err == nil {
		t.Fatal("the access token works after its hour")
	}
	if _, err := e.connections.Refresh(tokens.Refresh, ""); err != nil {
		t.Fatalf("Refresh() after the hour = %v", err)
	}
}

// TestAConnectionUnusedFor90DaysEnds, and every use starts the 90 days
// again.
func TestAConnectionUnusedFor90DaysEnds(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	tokens := e.connect(t, model.DefaultRights)
	for range 3 {
		e.clock.Add(80 * 24 * time.Hour)
		next, err := e.connections.Refresh(tokens.Refresh, "")
		if err != nil {
			t.Fatalf("Refresh() 80 days after the last use = %v", err)
		}
		tokens = next
	}
	e.clock.Add(model.ConnectionIdle)
	if _, err := e.connections.Refresh(tokens.Refresh, ""); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("Refresh() after 90 days unused = %v, want invalid", err)
	}
	if !e.recorded(t, "connection.ended", "unused for 90 days") {
		t.Fatal("the activity does not say the connection ended unused")
	}
}

// TestAnIdleConnectionEndsWhenListed: Settings never shows a connection
// unused for 90 days as live, and it does not count toward the limit.
func TestAnIdleConnectionEndsWhenListed(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	e.connect(t, model.DefaultRights)
	e.clock.Add(model.ConnectionIdle + time.Hour)
	fresh := e.connect(t, model.DefaultRights)
	connections, err := e.connections.List(e.user.User.ID)
	if err != nil || len(connections) != 1 || connections[0].ID != fresh.Connection.ID {
		t.Fatalf("List() = %+v, %v", connections, err)
	}
	var live int
	if err := e.database.Get(&live, `SELECT COUNT(*) FROM connections WHERE ended_at IS NULL`); err != nil || live != 1 {
		t.Fatalf("live connections = %d, %v", live, err)
	}
	if !e.recorded(t, "connection.ended", "unused for 90 days") {
		t.Fatal("the activity does not say the connection ended unused")
	}
}

// TestRemovingAConnectionStopsItsAccessAtOnce, and a change of rights
// counts from the next request.
func TestRemovingAConnectionStopsItsAccessAtOnce(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	tokens := e.connect(t, model.DefaultRights)
	id := tokens.Connection.ID

	if _, err := e.connections.SetRights(e.user.User.ID, id, model.RightDelete, nil, Source{Via: ViaWeb}); err != nil {
		t.Fatal(err)
	}
	bearer, err := e.connections.Authenticate(tokens.Access)
	if err != nil || bearer.Rights != model.RightRead|model.RightDelete {
		t.Fatalf("rights after the change = %v, %v, want read and delete", bearer, err)
	}
	if !e.recorded(t, "connection.rights_changed", "read,delete") {
		t.Fatal("the activity misses the change of rights")
	}

	// Grace cannot change or remove Ada's connection.
	testutil.SeedUser(t, e.database, testutil.UserOptions{ID: "grace", Verified: true})
	if _, err := e.connections.SetRights("grace", id, model.RightServers, nil, Source{}); !errors.Is(err, repository.ErrConnectionNotFound) {
		t.Fatalf("SetRights() by another person = %v, want not found", err)
	}
	if err := e.connections.Remove("grace", id, Source{}); !errors.Is(err, repository.ErrConnectionNotFound) {
		t.Fatalf("Remove() by another person = %v, want not found", err)
	}

	if err := e.connections.Remove(e.user.User.ID, strings.ReplaceAll(id, "-", ""), Source{Via: ViaWeb}); err != nil {
		t.Fatalf("Remove() by its ID without dashes = %v", err)
	}
	if _, err := e.connections.Authenticate(tokens.Access); err == nil {
		t.Fatal("the access token works after the connection was removed")
	}
	if _, err := e.connections.Refresh(tokens.Refresh, ""); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("Refresh() after Remove() = %v, want invalid", err)
	}
	if err := e.connections.Remove(e.user.User.ID, id, Source{}); !errors.Is(err, repository.ErrConnectionNotFound) {
		t.Fatalf("second Remove() = %v, want not found", err)
	}
}

// TestRevokeEndsTheConnectionOfEitherToken (RFC 7009); a token the hub
// does not know is no error.
func TestRevokeEndsTheConnectionOfEitherToken(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	byRefresh, byAccess := e.connect(t, model.DefaultRights), e.connect(t, model.DefaultRights)
	if err := e.connections.Revoke(byRefresh.Refresh, Source{Via: ViaCLI}); err != nil {
		t.Fatal(err)
	}
	if err := e.connections.Revoke(byAccess.Access, Source{Via: ViaCLI}); err != nil {
		t.Fatal(err)
	}
	for _, tokens := range []Tokens{byRefresh, byAccess} {
		if _, err := e.connections.Authenticate(tokens.Access); err == nil {
			t.Fatal("an access token works after Revoke()")
		}
	}
	for _, raw := range []string{"", "rt_unknown", "at_unknown", "pat_x"} {
		if err := e.connections.Revoke(raw, Source{}); err != nil {
			t.Errorf("Revoke(%q) = %v, want nil", raw, err)
		}
	}
	if !e.recorded(t, "connection.ended", "logged out") {
		t.Fatal("the activity misses the logout")
	}
}

// TestRefreshNeedsTheSameClient: a connection an OAuth client made
// refreshes only for that client.
func TestRefreshNeedsTheSameClient(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	connection, err := e.connections.Create(NewConnection{UserID: e.user.User.ID, WorkspaceID: e.user.Workspace.ID, ClientName: "Claude", Rights: model.DefaultRights, ClientID: "client-1", Resource: "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := e.connections.Issue(connection)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"", "client-2"} {
		if _, err := e.connections.Refresh(tokens.Refresh, client); !errors.Is(err, ErrRefreshInvalid) {
			t.Errorf("Refresh() for client %q = %v, want invalid", client, err)
		}
	}
	bearer, err := e.connections.Authenticate(tokens.Access)
	if err != nil || bearer.Resource != "/mcp" {
		t.Fatalf("Authenticate() = %+v, %v, want the resource /mcp", bearer, err)
	}
	if _, err := e.connections.Refresh(tokens.Refresh, "client-1"); err != nil {
		t.Fatalf("Refresh() for its client = %v", err)
	}
}

// TestEndAllForUserEndsEveryConnection, the admin kill switch, and keeps
// those of others.
func TestEndAllForUserEndsEveryConnection(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	first, second := e.connect(t, model.DefaultRights), e.connect(t, model.RightServers)
	if err := e.connections.EndAllForUser(e.user.User.ID); err != nil {
		t.Fatal(err)
	}
	for _, tokens := range []Tokens{first, second} {
		if _, err := e.connections.Authenticate(tokens.Access); err == nil {
			t.Fatal("an access token works after EndAllForUser()")
		}
	}
}

// TestRecoveryKeyEndsConnectionsAndCITokens: a recovery key throws out
// what a stolen GitHub account let in. Every connection of the account,
// an AI agent's OAuth grant included, and every CI token stop working, and
// the activity says why. Those of another account stay.
func TestRecoveryKeyEndsConnectionsAndCITokens(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	e.auth.UseBearers(e.connections, NewCITokenService(repository.NewCITokens(e.database), repository.NewApps(e.database, e.database), e.workspaceService))
	ciTokens := e.auth.ciTokens
	other := testutil.SeedUser(t, e.database, testutil.UserOptions{ID: "bob", Email: "bob@example.com", Name: "Bob", Verified: true})
	cli := e.connect(t, model.DefaultRights)
	agent, err := e.connections.Create(NewConnection{UserID: e.user.User.ID, WorkspaceID: e.user.Workspace.ID, ClientName: "Claude", Rights: model.DefaultRights, ClientID: "client-1", Resource: "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	agentTokens, err := e.connections.Issue(agent)
	if err != nil {
		t.Fatal(err)
	}
	_, ci, err := ciTokens.Create(Actor{WorkspaceID: e.user.Workspace.ID, UserID: e.user.User.ID}, NewCIToken{Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	bobConnection, err := e.connections.Create(NewConnection{UserID: other.User.ID, WorkspaceID: other.Workspace.ID, ClientName: "moin CLI", Rights: model.DefaultRights})
	if err != nil {
		t.Fatal(err)
	}
	bobTokens, err := e.connections.Issue(bobConnection)
	if err != nil {
		t.Fatal(err)
	}
	_, bobCI, err := ciTokens.Create(Actor{WorkspaceID: other.Workspace.ID, UserID: other.User.ID}, NewCIToken{Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := e.auth.CreateRecoveryKey(e.user.User.Email)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.auth.SignInWithRecoveryKey(key); err != nil {
		t.Fatal(err)
	}

	for _, access := range []string{cli.Access, agentTokens.Access} {
		if _, err := e.connections.Authenticate(access); err == nil {
			t.Error("a connection works after the recovery key")
		}
	}
	if _, err := e.connections.Refresh(agentTokens.Refresh, "client-1"); err == nil {
		t.Error("the AI agent refreshed its tokens after the recovery key")
	}
	if _, err := ciTokens.Authenticate(ci); err == nil {
		t.Error("a CI token works after the recovery key")
	}
	if _, err := e.connections.Authenticate(bobTokens.Access); err != nil {
		t.Errorf("the connection of another account ended: %v", err)
	}
	if _, err := ciTokens.Authenticate(bobCI); err != nil {
		t.Errorf("the CI token of another account was removed: %v", err)
	}
	events, err := e.activity.ListByWorkspace(e.user.Workspace.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	ended, removed := 0, 0
	for _, event := range events {
		if !strings.Contains(event.Metadata, "a recovery key was used") {
			continue
		}
		switch event.Action {
		case "connection.ended":
			ended++
		case "ci_token.removed":
			removed++
		}
	}
	if ended != 2 || removed != 1 {
		t.Fatalf("activity: %d connections ended, %d CI tokens removed, want 2 and 1", ended, removed)
	}
}

// TestRightsMustExist and read is always in.
func TestRightsMustExist(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	if _, err := e.connections.Create(NewConnection{UserID: e.user.User.ID, WorkspaceID: e.user.Workspace.ID, Rights: 1 << 7}); !errors.Is(err, ErrRightsUnknown) {
		t.Fatalf("Create() with a right that does not exist = %v", err)
	}
	connection, err := e.connections.Create(NewConnection{UserID: e.user.User.ID, WorkspaceID: e.user.Workspace.ID, ClientName: " \x00 "})
	if err != nil || connection.Rights != model.RightRead || connection.ClientName != "A client" {
		t.Fatalf("Create() without rights or name = %+v, %v", connection, err)
	}
	if rights, ok := model.ParseRights("deploy", "env,servers"); !ok || rights.String() != "read,deploy,env,servers" {
		t.Fatalf("ParseRights() = %v, %v", rights, ok)
	}
	if _, ok := model.ParseRights("write"); ok {
		t.Fatal("ParseRights() takes write")
	}
	if !slices.Equal((model.RightServers | model.RightDelete).List(), []string{"delete", "servers"}) {
		t.Fatal("List() is not in the order of the rights")
	}
}

// recorded reports whether the workspace activity has action with text in
// its details.
func (e deviceLoginEnvironment) recorded(t *testing.T, action, text string) bool {
	t.Helper()
	events, err := e.activity.ListByWorkspace(e.user.Workspace.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Action == action && strings.Contains(event.Metadata, text) {
			return true
		}
	}
	return false
}
