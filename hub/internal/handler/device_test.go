package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jmoiron/sqlx"
)

type deviceHandlerTest struct {
	handler     *DeviceHandler
	logins      *service.DeviceLoginService
	connections *service.ConnectionService
	workspaces  *service.WorkspaceService
	auth        *service.AuthService
	passkeys    *service.PasskeyService
	owner       testutil.UserFixture
	db          *sqlx.DB
}

// newDeviceHandlerTest is the approval page of a hub; with passkeys, the
// hub offers them.
func newDeviceHandlerTest(t *testing.T, passkeys bool) deviceHandlerTest {
	t.Helper()
	database, workspaces, owner, mailer := invitationHandlerTestSetup(t)
	users := repository.NewUsers(database)
	email := service.NewEmailService(mailer, "https://app.example.com", "Test App")
	var webAuthn *webauthn.WebAuthn
	if passkeys {
		var err error
		webAuthn, err = webauthn.New(&webauthn.Config{RPID: "localhost", RPDisplayName: "moin", RPOrigins: []string{"http://localhost:8090"}, AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired}})
		if err != nil {
			t.Fatal(err)
		}
	}
	passkeyService := service.NewPasskeyService(repository.NewPasskeys(database), users, email, webAuthn)
	connections := service.NewConnectionService(repository.NewConnections(database), workspaces)
	logins := service.NewDeviceLoginService(repository.NewDeviceLogins(database), connections, passkeyService)
	auth := service.NewAuthService(users, repository.NewTokens(database), repository.NewSessions(database), workspaces, email, false, time.Hour, time.Hour, time.Hour)
	return deviceHandlerTest{
		handler:     NewDeviceHandler(logins, workspaces, passkeyService, auth, &config.Config{}),
		logins:      logins,
		connections: connections,
		workspaces:  workspaces,
		auth:        auth,
		passkeys:    passkeyService,
		owner:       owner,
		db:          database,
	}
}

// session is a session of the owner that signed in age ago.
func (test deviceHandlerTest) session(t *testing.T, age time.Duration) *model.Session {
	t.Helper()
	session, _, err := test.auth.CreateSession(httptest.NewRequest(http.MethodGet, "/", nil), test.owner.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	session.CreatedAt = time.Now().Add(-age)
	return session
}

func deviceRequest(method, target string, form url.Values, user *model.User, session *model.Session) *http.Request {
	var request *http.Request
	if form == nil {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{})
	if user != nil {
		ctx = ctxkeys.WithUser(ctx, user)
	}
	if session != nil {
		ctx = ctxkeys.WithSession(ctx, session)
	}
	return request.WithContext(ctx)
}

func deviceCookie(recorder *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == pendingDeviceCookie {
			return cookie
		}
	}
	return nil
}

func TestDevicePageSignsInFirstAndComesBack(t *testing.T) {
	test := newDeviceHandlerTest(t, false)
	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, "/device?code="+strings.ToLower(started.UserCode), nil, nil, nil))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth" {
		t.Fatalf("guest: status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	cookie := deviceCookie(recorder)
	if cookie == nil || cookie.Value != service.NormalizeUserCode(started.UserCode) {
		t.Fatalf("pending device cookie = %#v", cookie)
	}

	// Any sign-in ends in redirectAfterAuth, which goes back to the page.
	auth := service.NewAuthService(nil, nil, nil, test.workspaces, nil, false, time.Hour, time.Hour, time.Hour)
	signIn := deviceRequest(http.MethodGet, "/auth/onboarding", nil, test.owner.User, nil)
	signIn.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	NewAuthHandler(auth, test.workspaces, nil, nil, &config.Config{}).OnboardingPage(recorder, signIn)
	want := "/device?code=" + started.UserCode
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != want {
		t.Fatalf("after sign-in: status %d, location %q, want %q", recorder.Code, recorder.Header().Get("Location"), want)
	}

	recorder = httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, want, nil, test.owner.User, test.session(t, 0)))
	body := recorder.Body.String()
	for _, part := range []string{"moin CLI on host-1", started.UserCode, "Read: apps, logs, status", "Secrets and data", `name="rights" value="deploy" checked`, `name="rights" value="env" checked`, `name="rights" value="delete"`, `name="rights" value="servers"`, ">Allow<"} {
		if !strings.Contains(body, part) {
			t.Fatalf("signed in: the page misses %q: status %d, body %s", part, recorder.Code, body)
		}
	}
	for _, part := range []string{`value="delete" checked`, `value="servers" checked`, `data-passkey="confirm"`} {
		if strings.Contains(body, part) {
			t.Fatalf("signed in: the page has %q", part)
		}
	}
	if cookie := deviceCookie(recorder); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("the page did not clear the pending cookie: %#v", cookie)
	}
}

func TestDevicePageApprovesWithTheRightsPicked(t *testing.T) {
	test := newDeviceHandlerTest(t, false)
	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	// A right that does not exist is an error, not left out.
	unknown := url.Values{"code": {started.UserCode}, "workspace_id": {test.owner.Workspace.ID}, "rights": {"deploy", "root"}}
	recorder := httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", unknown, test.owner.User, test.session(t, time.Minute)))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "Choose from the rights shown.") {
		t.Fatalf("an unknown right: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	form := url.Values{"code": {started.UserCode}, "workspace_id": {test.owner.Workspace.ID}, "rights": {"deploy", "servers"}}
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User, test.session(t, time.Minute)))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "is connected") {
		t.Fatalf("approve: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	tokens, err := test.logins.Token(started.DeviceCode)
	if err != nil || tokens.Connection.WorkspaceID != test.owner.Workspace.ID || tokens.Connection.Rights != model.RightRead|model.RightDeploy|model.RightServers {
		t.Fatalf("Token() = %+v, %v", tokens, err)
	}

	// The code is used now: the page asks for a new one.
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User, test.session(t, time.Minute)))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "No login waits for this code") {
		t.Fatalf("second approve: status %d, body %s", recorder.Code, recorder.Body.String())
	}
}

// TestDevicePageAsksToSignInAgain: without a passkey, a sign-in older than
// five minutes allows nothing; the page signs out and comes back after a
// new sign-in.
func TestDevicePageAsksToSignInAgain(t *testing.T) {
	test := newDeviceHandlerTest(t, false)
	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	old := test.session(t, service.FreshSignIn+time.Minute)
	recorder := httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, "/device?code="+started.UserCode, nil, test.owner.User, old))
	if body := recorder.Body.String(); !strings.Contains(body, "Sign in again to allow") || !strings.Contains(body, `formaction="/device/reauth"`) {
		t.Fatalf("old sign-in: body %s", body)
	}
	form := url.Values{"code": {started.UserCode}, "workspace_id": {test.owner.Workspace.ID}}
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User, old))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "older than five minutes") {
		t.Fatalf("approve with an old sign-in: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if connections, _ := test.connections.List(test.owner.User.ID); len(connections) != 0 {
		t.Fatalf("an old sign-in made a connection: %+v", connections)
	}

	recorder = httptest.NewRecorder()
	test.handler.Reauth(recorder, deviceRequest(http.MethodPost, "/device/reauth", form, test.owner.User, old))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth" {
		t.Fatalf("reauth: status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if cookie := deviceCookie(recorder); cookie == nil || cookie.Value != service.NormalizeUserCode(started.UserCode) {
		t.Fatalf("reauth kept no pending code: %#v", cookie)
	}
}

// TestDevicePageNeedsThePasskey: an account with a passkey allows only
// with it; the page asks for it, and Allow without it is refused.
func TestDevicePageNeedsThePasskey(t *testing.T) {
	test := newDeviceHandlerTest(t, true)
	fresh := test.session(t, 0)
	if err := repository.NewPasskeys(test.db).Create(&model.Passkey{ID: "p1", UserID: test.owner.User.ID, CredentialID: []byte{1}, Credential: "{}", Name: "Passkey", CreatedAt: time.Now()}, fresh.ID); err != nil {
		t.Fatal(err)
	}
	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, "/device?code="+started.UserCode, nil, test.owner.User, fresh))
	if body := recorder.Body.String(); !strings.Contains(body, "Allow with passkey") || !strings.Contains(body, `data-passkey="confirm"`) || !strings.Contains(body, `name="passkey"`) || strings.Contains(body, "Add a passkey") {
		t.Fatalf("with a passkey: body %s", body)
	}
	form := url.Values{"code": {started.UserCode}, "workspace_id": {test.owner.Workspace.ID}}
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User, fresh))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "Allow with your passkey.") {
		t.Fatalf("approve without the passkey: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	form.Set("passkey", `{"id":"forged"}`)
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User, fresh))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "This took too long") {
		t.Fatalf("approve with an answer to no ceremony: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if _, err := test.logins.Pending(started.UserCode); err != nil {
		t.Fatalf("the login stays pending: %v", err)
	}
}

func TestDevicePageDenies(t *testing.T) {
	test := newDeviceHandlerTest(t, false)
	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	test.handler.Deny(recorder, deviceRequest(http.MethodPost, "/device/deny", url.Values{"code": {started.UserCode}}, test.owner.User, test.session(t, 0)))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Not connected") {
		t.Fatalf("deny: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if _, err := test.logins.Token(started.DeviceCode); !errors.Is(err, service.ErrDeviceLoginDenied) {
		t.Fatalf("Token() after deny = %v", err)
	}
}

func TestDevicePageRefusesWrongCodesAndWorkspaces(t *testing.T) {
	test := newDeviceHandlerTest(t, false)
	session := test.session(t, 0)
	recorder := httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, "/device?code=WRNG-CODE", nil, test.owner.User, session))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "No login waits for this code") || !strings.Contains(recorder.Body.String(), `aria-invalid="true"`) {
		t.Fatalf("wrong code: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, "/device", nil, test.owner.User, session))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Enter the code your terminal shows") {
		t.Fatalf("no code: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"code": {started.UserCode}, "workspace_id": {"workspace-of-someone-else"}}
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User, session))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "Choose one of your workspaces") {
		t.Fatalf("foreign workspace: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if _, err := test.logins.Pending(started.UserCode); err != nil {
		t.Fatalf("the login stays pending: %v", err)
	}
}
