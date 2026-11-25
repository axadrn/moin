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

type ciTokenEnvironment struct {
	deviceLoginEnvironment
	tokens *CITokenService
	owner  *model.WorkspaceMember
	member *model.WorkspaceMember
}

// newCITokenEnvironment is Ada, the owner of her workspace, Grace, a
// member there, and the app shop.
func newCITokenEnvironment(t *testing.T) ciTokenEnvironment {
	t.Helper()
	e := newDeviceLoginEnvironment(t)
	grace := testutil.SeedUser(t, e.database, testutil.UserOptions{ID: "grace", Name: "Grace", Verified: true})
	e.database.MustExec(`INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at) VALUES ('grace-in-ada', $1, $2, 'member', $3)`, e.user.Workspace.ID, grace.User.ID, time.Now())
	e.database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at) VALUES ('server-1', $1, 'web-1', $2)`, e.user.Workspace.ID, time.Now())
	e.database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at) VALUES ('11111111-1111-1111-1111-111111111111', $1, 'server-1', 'shop', 'shop', $2)`, e.user.Workspace.ID, time.Now())
	tokens := NewCITokenService(repository.NewCITokens(e.database), repository.NewApps(e.database, e.database), e.workspaceService)
	tokens.now = e.clock.Now
	members := repository.NewWorkspaceMembers(e.database)
	owner, err := members.ByWorkspaceAndUser(e.user.Workspace.ID, e.user.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := members.ByWorkspaceAndUser(e.user.Workspace.ID, grace.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ciTokenEnvironment{deviceLoginEnvironment: e, tokens: tokens, owner: owner, member: member}
}

func actorOf(member *model.WorkspaceMember) Actor {
	return Actor{WorkspaceID: member.WorkspaceID, UserID: member.UserID, Source: Source{Via: ViaWeb}}
}

func TestCITokenActsWithItsRights(t *testing.T) {
	e := newCITokenEnvironment(t)
	token, raw, err := e.tokens.Create(actorOf(e.owner), NewCIToken{Name: "  shop deploy ", Rights: model.RightDeploy, App: "shop", ExpiresInDays: 7})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !strings.HasPrefix(raw, CITokenMarker) || len(raw) != len(CITokenMarker)+52 || token.Prefix != raw[4:12] || token.Name != "shop deploy" {
		t.Fatalf("token = %+v, raw %q", token, raw)
	}
	bearer, err := e.tokens.Authenticate(raw)
	if err != nil || bearer.Rights != model.RightRead|model.RightDeploy || bearer.AppID != "11111111-1111-1111-1111-111111111111" || bearer.CITokenID != token.ID || bearer.Kind() != "CI token" {
		t.Fatalf("Authenticate() = %+v, %v", bearer, err)
	}
	if !e.recorded(t, "ci_token.created", `"app":"shop"`) || e.recorded(t, "ci_token.created", raw) {
		t.Fatal("the activity misses the token, or holds it")
	}
	e.clock.Add(7 * 24 * time.Hour)
	if _, err := e.tokens.Authenticate(raw); !errors.Is(err, repository.ErrCITokenNotFound) {
		t.Fatalf("Authenticate() after its expiry = %v, want not found", err)
	}
	for _, wrong := range []string{"", "pat_", "pat_unknown", "at_" + raw[4:]} {
		if _, err := e.tokens.Authenticate(wrong); err == nil {
			t.Errorf("Authenticate(%q) works", wrong)
		}
	}
}

func TestCITokenRefusesWrongInput(t *testing.T) {
	e := newCITokenEnvironment(t)
	for name, input := range map[string]NewCIToken{
		"no name":         {Name: " "},
		"a long name":     {Name: strings.Repeat("a", 65)},
		"a control":       {Name: "c\ni"},
		"a long expiry":   {Name: "ci", ExpiresInDays: 366},
		"a minus expiry":  {Name: "ci", ExpiresInDays: -1},
		"an unknown app":  {Name: "ci", App: "blog"},
		"a made-up right": {Name: "ci", Rights: 1 << 7},
	} {
		if _, _, err := e.tokens.Create(actorOf(e.owner), input); err == nil {
			t.Errorf("Create() with %s worked", name)
		}
	}
	stranger := testutil.SeedUser(t, e.database, testutil.UserOptions{ID: "stranger", Verified: true})
	if _, _, err := e.tokens.Create(Actor{WorkspaceID: stranger.Workspace.ID, UserID: e.user.User.ID}, NewCIToken{Name: "ci"}); !errors.Is(err, ErrConnectionWorkspace) {
		t.Fatalf("Create() in another workspace = %v, want ErrConnectionWorkspace", err)
	}
}

func TestCITokenLimit(t *testing.T) {
	e := newCITokenEnvironment(t)
	for range MaxCITokensPerUser {
		if _, _, err := e.tokens.Create(actorOf(e.member), NewCIToken{Name: "ci"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := e.tokens.Create(actorOf(e.member), NewCIToken{Name: "ci"}); !errors.Is(err, ErrCITokenLimit) {
		t.Fatalf("Create() over the limit = %v, want ErrCITokenLimit", err)
	}
}

// TestOwnersSeeEveryCIToken of the workspace and may remove any; members
// see and remove their own; only the maker changes rights.
func TestOwnersSeeEveryCIToken(t *testing.T) {
	e := newCITokenEnvironment(t)
	adas, _, err := e.tokens.Create(actorOf(e.owner), NewCIToken{Name: "ada ci", Rights: model.DefaultRights})
	if err != nil {
		t.Fatal(err)
	}
	graces, graceRaw, err := e.tokens.Create(actorOf(e.member), NewCIToken{Name: "grace ci", Rights: model.DefaultRights})
	if err != nil {
		t.Fatal(err)
	}
	names := func(member *model.WorkspaceMember) []string {
		tokens, err := e.tokens.List(member)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, token := range tokens {
			names = append(names, token.Name+" by "+token.MakerName)
		}
		slices.Sort(names)
		return names
	}
	if got := names(e.owner); !slices.Equal(got, []string{"ada ci by Ada", "grace ci by Grace"}) {
		t.Fatalf("the owner sees %v", got)
	}
	if got := names(e.member); !slices.Equal(got, []string{"grace ci by Grace"}) {
		t.Fatalf("the member sees %v", got)
	}

	if _, err := e.tokens.SetRights(actorOf(e.owner), graces.ID, model.RightServers, nil); !errors.Is(err, ErrCITokenMaker) {
		t.Fatalf("SetRights() by the owner on Grace's token = %v, want ErrCITokenMaker", err)
	}
	changed, err := e.tokens.SetRights(actorOf(e.member), graces.ID, model.RightEnv, nil)
	if err != nil || changed.Rights != model.RightRead|model.RightEnv {
		t.Fatalf("SetRights() by its maker = %+v, %v", changed, err)
	}
	bearer, err := e.tokens.Authenticate(graceRaw)
	if err != nil || bearer.Rights != model.RightRead|model.RightEnv {
		t.Fatalf("rights after the change = %+v, %v", bearer, err)
	}

	if err := e.tokens.Remove(e.member, adas.ID, Source{}); !errors.Is(err, repository.ErrCITokenNotFound) {
		t.Fatalf("Remove() of the owner's token by a member = %v, want not found", err)
	}
	if err := e.tokens.Remove(e.owner, graces.ID, Source{Via: ViaWeb}); err != nil {
		t.Fatalf("Remove() of a member's token by the owner = %v", err)
	}
	if _, err := e.tokens.Authenticate(graceRaw); err == nil {
		t.Fatal("a removed CI token works")
	}
}

// TestRevokeAllAccessEndsConnectionsAndCITokens: the admin kill switch
// ends every session, connection and CI token of the person, and nothing
// of anyone else.
func TestRevokeAllAccessEndsConnectionsAndCITokens(t *testing.T) {
	e := newCITokenEnvironment(t)
	connection := e.connect(t, model.DefaultRights)
	_, adaRaw, err := e.tokens.Create(actorOf(e.owner), NewCIToken{Name: "ada ci"})
	if err != nil {
		t.Fatal(err)
	}
	_, graceRaw, err := e.tokens.Create(actorOf(e.member), NewCIToken{Name: "grace ci"})
	if err != nil {
		t.Fatal(err)
	}
	e.session(t, e.user.User.ID)
	if err := e.auth.RevokeAllAccess(e.user.User.ID); !errors.Is(err, errBearersNotConfigured) {
		t.Fatalf("RevokeAllAccess() unwired = %v", err)
	}
	e.auth.UseBearers(e.connections, e.tokens)
	if err := e.auth.RevokeAllAccess(e.user.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.connections.Authenticate(connection.Access); err == nil {
		t.Fatal("the connection works after RevokeAllAccess()")
	}
	if _, err := e.tokens.Authenticate(adaRaw); err == nil {
		t.Fatal("the CI token works after RevokeAllAccess()")
	}
	if _, err := e.tokens.Authenticate(graceRaw); err != nil {
		t.Fatalf("Grace's CI token stopped: %v", err)
	}
	sessions, err := e.auth.Sessions(e.user.User.ID)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("sessions left = %d, %v", len(sessions), err)
	}
}
