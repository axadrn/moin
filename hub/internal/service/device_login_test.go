package service

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// deviceClock is a clock the tests move by hand.
type deviceClock struct{ now time.Time }

func (c *deviceClock) Now() time.Time { return c.now }

func (c *deviceClock) Add(d time.Duration) { c.now = c.now.Add(d) }

type deviceLoginEnvironment struct {
	passkeyEnvironment
	logins      *DeviceLoginService
	connections *ConnectionService
	clock       *deviceClock
	user        testutil.UserFixture
	activity    *repository.Activity
}

// newDeviceLoginEnvironment is a hub with passkeys, Ada and a clock the
// tests move.
func newDeviceLoginEnvironment(t *testing.T) deviceLoginEnvironment {
	t.Helper()
	environment := newPasskeyEnvironment(t)
	user := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Name: "Ada", Verified: true})
	clock := &deviceClock{now: time.Now().UTC()}
	connections := NewConnectionService(repository.NewConnections(environment.database), environment.workspaceService)
	connections.now = clock.Now
	environment.passkeys.now = clock.Now
	logins := NewDeviceLoginService(repository.NewDeviceLogins(environment.database), connections, environment.passkeys)
	logins.now = clock.Now
	return deviceLoginEnvironment{passkeyEnvironment: environment, logins: logins, connections: connections, clock: clock, user: user, activity: repository.NewActivity(environment.database)}
}

func (e deviceLoginEnvironment) start(t *testing.T) StartedDeviceLogin {
	t.Helper()
	started, err := e.logins.Start("host-1")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return started
}

// fresh is a session of Ada that signed in just now.
func (e deviceLoginEnvironment) fresh(t *testing.T) *model.Session {
	t.Helper()
	session := e.session(t, e.user.User.ID)
	session.CreatedAt = e.clock.now
	return session
}

// approve approves the code as Ada, signed in just now, without a passkey.
func (e deviceLoginEnvironment) approve(code string, rights model.Rights, session *model.Session) (*model.Connection, error) {
	_, connection, err := e.logins.Approve(Approval{UserCode: code, WorkspaceID: e.user.Workspace.ID, Rights: rights, Presence: Presence{Session: session}, Source: Source{Via: ViaWeb}})
	return connection, err
}

// poll waits the interval and polls once.
func (e deviceLoginEnvironment) poll(started StartedDeviceLogin) (Tokens, error) {
	e.clock.Add(time.Duration(started.Interval) * time.Second)
	return e.logins.Token(started.DeviceCode)
}

func TestDeviceLoginGivesTheTokensOnceAfterApproval(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	started := e.start(t)
	if len(started.UserCode) != 9 || started.UserCode[4] != '-' || strings.ContainsAny(started.UserCode, "01OIL") {
		t.Fatalf("user code = %q, want ABCD-EFGH without look-alikes", started.UserCode)
	}
	if started.Interval != DeviceLoginInterval || !started.ExpiresAt.Equal(e.clock.now.Add(DeviceLoginLifetime)) {
		t.Fatalf("interval %d, expires %s", started.Interval, started.ExpiresAt)
	}
	if _, err := e.poll(started); !errors.Is(err, ErrDeviceLoginPending) {
		t.Fatalf("poll before approval = %v, want pending", err)
	}

	// The user types the code in any case, with or without the dash.
	pending, err := e.logins.Pending(strings.ToLower(strings.ReplaceAll(started.UserCode, "-", "")))
	if err != nil || pending.Hostname != "host-1" {
		t.Fatalf("Pending() = %+v, %v", pending, err)
	}
	session := e.fresh(t)
	connection, err := e.approve(started.UserCode, model.RightDeploy|model.RightServers, session)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if connection.ClientName != "moin CLI on host-1" || connection.Rights != model.RightRead|model.RightDeploy|model.RightServers || connection.WorkspaceID != e.user.Workspace.ID {
		t.Fatalf("connection = %+v", connection)
	}
	if _, err := e.approve(started.UserCode, model.DefaultRights, session); !errors.Is(err, ErrUserCodeNotFound) {
		t.Fatalf("second Approve() = %v, want the code used", err)
	}

	tokens, err := e.poll(started)
	if err != nil {
		t.Fatalf("poll after approval error = %v", err)
	}
	if !strings.HasPrefix(tokens.Access, AccessTokenMarker) || !strings.HasPrefix(tokens.Refresh, RefreshTokenMarker) || tokens.ExpiresIn != time.Hour || tokens.Connection.ID != connection.ID {
		t.Fatalf("tokens = %+v", tokens)
	}
	bearer, err := e.connections.Authenticate(tokens.Access)
	if err != nil || bearer.ConnectionID != connection.ID || bearer.Rights != connection.Rights || bearer.WorkspaceID != e.user.Workspace.ID {
		t.Fatalf("Authenticate() = %+v, %v", bearer, err)
	}
	if _, err := e.poll(started); !errors.Is(err, ErrDeviceLoginInvalid) {
		t.Fatalf("poll after the tokens = %v, want invalid", err)
	}
	events, err := e.activity.ListByWorkspace(e.user.Workspace.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Action == "connection.approved" && strings.Contains(event.Metadata, `"rights":"read,deploy,servers"`) {
			found = true
		}
		if strings.Contains(event.Metadata, tokens.Access) || strings.Contains(event.Metadata, tokens.Refresh) {
			t.Fatalf("the activity holds a token: %s", event.Metadata)
		}
	}
	if !found {
		t.Fatalf("no activity for the approval in %+v", events)
	}
}

func TestDeviceLoginDenied(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	started := e.start(t)
	if _, err := e.logins.Deny(started.UserCode); err != nil {
		t.Fatalf("Deny() error = %v", err)
	}
	if _, err := e.approve(started.UserCode, model.DefaultRights, e.fresh(t)); !errors.Is(err, ErrUserCodeNotFound) {
		t.Fatalf("Approve() after Deny() = %v, want not found", err)
	}
	if _, err := e.poll(started); !errors.Is(err, ErrDeviceLoginDenied) {
		t.Fatalf("poll = %v, want denied", err)
	}
	if _, err := e.poll(started); !errors.Is(err, ErrDeviceLoginInvalid) {
		t.Fatalf("poll after denied = %v, want invalid", err)
	}
}

func TestDeviceLoginExpires(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	started := e.start(t)
	e.clock.Add(DeviceLoginLifetime)
	if _, err := e.approve(started.UserCode, model.DefaultRights, e.fresh(t)); !errors.Is(err, ErrUserCodeNotFound) {
		t.Fatalf("Approve() after expiry = %v, want not found", err)
	}
	if _, err := e.poll(started); !errors.Is(err, ErrDeviceLoginExpired) {
		t.Fatalf("poll after expiry = %v, want expired", err)
	}
}

func TestDeviceLoginSlowsDownFastPolls(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	started := e.start(t)
	if _, err := e.poll(started); !errors.Is(err, ErrDeviceLoginPending) {
		t.Fatalf("first poll = %v", err)
	}
	e.clock.Add(time.Second)
	if _, err := e.logins.Token(started.DeviceCode); !errors.Is(err, ErrDeviceLoginSlowDown) {
		t.Fatalf("fast poll = %v, want slow_down", err)
	}
	// The interval grew to ten seconds: five are too few now.
	e.clock.Add(5 * time.Second)
	if _, err := e.logins.Token(started.DeviceCode); !errors.Is(err, ErrDeviceLoginSlowDown) {
		t.Fatalf("poll after five seconds = %v, want slow_down", err)
	}
	e.clock.Add(15 * time.Second)
	if _, err := e.logins.Token(started.DeviceCode); !errors.Is(err, ErrDeviceLoginPending) {
		t.Fatalf("poll after the longer interval = %v, want pending", err)
	}
}

func TestDeviceLoginRefusesWrongCodesAndForeignWorkspaces(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	started := e.start(t)
	for _, code := range []string{"", "ABCD-EFGH", "short", started.UserCode + "X", strings.Repeat("A", 200)} {
		if _, err := e.approve(code, model.DefaultRights, e.fresh(t)); !errors.Is(err, ErrUserCodeNotFound) {
			t.Errorf("Approve(%q) = %v, want not found", code, err)
		}
	}
	for _, code := range []string{"", "nope", started.DeviceCode + "0"} {
		if _, err := e.logins.Token(code); !errors.Is(err, ErrDeviceLoginInvalid) {
			t.Errorf("Token(%q) = %v, want invalid", code, err)
		}
	}
	// Another person's workspace is not one of Ada's.
	other := testutil.SeedUser(t, e.database, testutil.UserOptions{ID: "grace", Name: "Grace", Verified: true})
	_, _, err := e.logins.Approve(Approval{UserCode: started.UserCode, WorkspaceID: other.Workspace.ID, Rights: model.DefaultRights, Presence: Presence{Session: e.fresh(t)}})
	if !errors.Is(err, ErrConnectionWorkspace) {
		t.Fatalf("Approve() into a foreign workspace = %v, want ErrConnectionWorkspace", err)
	}
	if _, err := e.logins.Pending(started.UserCode); err != nil {
		t.Fatalf("the code stays pending after a refused approval: %v", err)
	}
	connections, err := e.connections.List(e.user.User.ID)
	if err != nil || len(connections) != 0 {
		t.Fatalf("a refused approval made a connection: %+v, %v", connections, err)
	}
}

// TestApprovalNeedsAFreshSignInWithoutAPasskey: an account without a passkey
// approves only with a sign-in less than five minutes old, and never while
// an operator impersonates it.
func TestApprovalNeedsAFreshSignInWithoutAPasskey(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	started := e.start(t)
	old := e.fresh(t)
	old.CreatedAt = e.clock.now.Add(-FreshSignIn - time.Second)
	if _, err := e.approve(started.UserCode, model.DefaultRights, old); !errors.Is(err, ErrPresenceSignIn) {
		t.Fatalf("Approve() with an old sign-in = %v, want ErrPresenceSignIn", err)
	}
	if _, err := e.approve(started.UserCode, model.DefaultRights, nil); !errors.Is(err, ErrImpersonating) {
		t.Fatalf("Approve() without a session = %v, want ErrImpersonating", err)
	}
	operator := "operator"
	impersonated := e.fresh(t)
	impersonated.ImpersonatorUserID = &operator
	if _, err := e.approve(started.UserCode, model.DefaultRights, impersonated); !errors.Is(err, ErrImpersonating) {
		t.Fatalf("Approve() while impersonating = %v, want ErrImpersonating", err)
	}
	if _, err := e.approve(started.UserCode, model.DefaultRights, e.fresh(t)); err != nil {
		t.Fatalf("Approve() with a fresh sign-in = %v", err)
	}
}

// TestApprovalNeedsThePasskeyRightThen: an account with a passkey approves
// only with an answer of its own passkey to a ceremony begun for it, with
// user verification. A fresh sign-in is not enough, an answer works once,
// and another account's passkey does not count.
func TestApprovalNeedsThePasskeyRightThen(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	session := e.fresh(t)
	authenticator := newSoftAuthenticator(t)
	e.add(t, session, authenticator)
	started := e.start(t)
	approve := func(presence Presence) error {
		_, _, err := e.logins.Approve(Approval{UserCode: started.UserCode, WorkspaceID: e.user.Workspace.ID, Rights: model.DefaultRights, Presence: presence})
		return err
	}
	confirm := func(session *model.Session, authenticator *softAuthenticator) Presence {
		t.Helper()
		options, ceremony, err := e.passkeys.BeginConfirm(session)
		if err != nil {
			t.Fatalf("BeginConfirm() error = %v", err)
		}
		answer, err := io.ReadAll(authenticator.get(t, options, passkeyOrigin))
		if err != nil {
			t.Fatal(err)
		}
		return Presence{Session: session, Ceremony: ceremony, Answer: answer}
	}

	if err := approve(Presence{Session: session}); !errors.Is(err, ErrPresencePasskey) {
		t.Fatalf("Approve() with a fresh sign-in but no passkey answer = %v, want ErrPresencePasskey", err)
	}
	authenticator.unverified = true
	if err := approve(confirm(session, authenticator)); !errors.Is(err, ErrPasskeyRefused) {
		t.Fatalf("Approve() without user verification = %v, want refused", err)
	}
	authenticator.unverified = false
	// Grace's passkey answers a ceremony begun for Ada: refused.
	grace := testutil.SeedUser(t, e.database, testutil.UserOptions{ID: "grace", Name: "Grace", Verified: true})
	graceSession := e.session(t, grace.User.ID)
	graceKey := newSoftAuthenticator(t)
	e.add(t, graceSession, graceKey)
	stolen := confirm(session, graceKey)
	if err := approve(stolen); !errors.Is(err, ErrPasskeyRefused) {
		t.Fatalf("Approve() with another account's passkey = %v, want refused", err)
	}

	presence := confirm(session, authenticator)
	if err := approve(presence); err != nil {
		t.Fatalf("Approve() with the passkey = %v", err)
	}
	// The same answer again: its ceremony is used up.
	again := e.start(t)
	_, _, err := e.logins.Approve(Approval{UserCode: again.UserCode, WorkspaceID: e.user.Workspace.ID, Rights: model.DefaultRights, Presence: presence})
	if !errors.Is(err, ErrPasskeyExpired) {
		t.Fatalf("Approve() with a used answer = %v, want ErrPasskeyExpired", err)
	}
}

// TestRemovedBeforeThePollIsDenied: a connection removed in Settings
// before the computer asked gives it no tokens.
func TestRemovedBeforeThePollIsDenied(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	started := e.start(t)
	connection, err := e.approve(started.UserCode, model.DefaultRights, e.fresh(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.connections.Remove(e.user.User.ID, connection.ID, Source{Via: ViaWeb}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.poll(started); !errors.Is(err, ErrDeviceLoginDenied) {
		t.Fatalf("poll after the connection was removed = %v, want denied", err)
	}
}

func TestDeviceClientName(t *testing.T) {
	for hostname, want := range map[string]string{
		"host-1":                 "moin CLI on host-1",
		"":                       "moin CLI",
		"bad\x00\nhost":          "moin CLI on badhost",
		strings.Repeat("a", 100): "moin CLI on " + strings.Repeat("a", 50),
	} {
		if got := DeviceClientName(cleanHostname(hostname)); got != want {
			t.Errorf("DeviceClientName(%q) = %q, want %q", hostname, got, want)
		}
	}
}

// TestApprovalChecksTheWorkspaceBeforeThePasskey: a workspace the person
// is not in, or a right that does not exist, fails before the ceremony, so
// the passkey answer is not spent.
func TestApprovalChecksTheWorkspaceBeforeThePasskey(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	session := e.fresh(t)
	authenticator := newSoftAuthenticator(t)
	e.add(t, session, authenticator)
	started := e.start(t)
	options, ceremony, err := e.passkeys.BeginConfirm(session)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := io.ReadAll(authenticator.get(t, options, passkeyOrigin))
	if err != nil {
		t.Fatal(err)
	}
	presence := Presence{Session: session, Ceremony: ceremony, Answer: answer}
	if _, _, err := e.logins.Approve(Approval{UserCode: started.UserCode, WorkspaceID: "elsewhere", Rights: model.DefaultRights, Presence: presence}); !errors.Is(err, ErrConnectionWorkspace) {
		t.Fatalf("Approve() in another workspace = %v, want ErrConnectionWorkspace", err)
	}
	if _, _, err := e.logins.Approve(Approval{UserCode: started.UserCode, WorkspaceID: e.user.Workspace.ID, Rights: model.DefaultRights | 1<<7, Presence: presence}); !errors.Is(err, ErrRightsUnknown) {
		t.Fatalf("Approve() with an unknown right = %v, want ErrRightsUnknown", err)
	}
	if _, _, err := e.logins.Approve(Approval{UserCode: started.UserCode, WorkspaceID: e.user.Workspace.ID, Rights: model.DefaultRights, Presence: presence}); err != nil {
		t.Fatalf("Approve() with the same answer in the right workspace and rights = %v", err)
	}
}

// TestPresentLastsFiveMinutes: an account without a passkey is present
// for five minutes after any sign-in; one with a passkey for five minutes
// after it confirmed with it on this session. Adding a passkey needs the
// same.
func TestPresentLastsFiveMinutes(t *testing.T) {
	e := newDeviceLoginEnvironment(t)
	session := e.fresh(t)
	if err := e.passkeys.Present(session); err != nil {
		t.Fatalf("Present() after signing in = %v", err)
	}
	e.clock.Add(FreshSignIn + time.Second)
	if err := e.passkeys.Present(session); !errors.Is(err, ErrPresenceSignIn) {
		t.Fatalf("Present() later = %v, want ErrPresenceSignIn", err)
	}
	if _, _, err := e.passkeys.BeginAdd(session); !errors.Is(err, ErrPresenceSignIn) {
		t.Fatalf("BeginAdd() on an old session = %v, want ErrPresenceSignIn", err)
	}

	session = e.fresh(t)
	authenticator := newSoftAuthenticator(t)
	e.add(t, session, authenticator)
	if err := e.passkeys.Present(session); !errors.Is(err, ErrPresencePasskey) {
		t.Fatalf("Present() with a passkey, not confirmed = %v, want ErrPresencePasskey", err)
	}
	options, ceremony, err := e.passkeys.BeginConfirm(session)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := io.ReadAll(authenticator.get(t, options, passkeyOrigin))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.passkeys.Confirm(Presence{Session: session, Ceremony: ceremony, Answer: answer}); err != nil {
		t.Fatalf("Confirm() = %v", err)
	}
	e.clock.Add(FreshSignIn - time.Second)
	if err := e.passkeys.Present(session); err != nil {
		t.Fatalf("Present() after confirming = %v", err)
	}
	if _, _, err := e.passkeys.BeginAdd(session); err != nil {
		t.Fatalf("BeginAdd() after confirming = %v", err)
	}
	e.clock.Add(2 * time.Second)
	if err := e.passkeys.Present(session); !errors.Is(err, ErrPresencePasskey) {
		t.Fatalf("Present() five minutes after confirming = %v, want ErrPresencePasskey", err)
	}
	admin := "admin"
	if err := e.passkeys.Present(&model.Session{ID: session.ID, UserID: session.UserID, ImpersonatorUserID: &admin}); !errors.Is(err, ErrImpersonating) {
		t.Fatalf("Present() impersonating = %v", err)
	}
}
