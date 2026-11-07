package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/config"
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

type deviceHandlerTest struct {
	handler    *DeviceHandler
	logins     *service.DeviceLoginService
	workspaces *service.WorkspaceService
	owner      testutil.UserFixture
}

func newDeviceHandlerTest(t *testing.T) deviceHandlerTest {
	t.Helper()
	database, workspaces, owner, _ := invitationHandlerTestSetup(t)
	tokens := service.NewAPITokenService(repository.NewAPITokens(database), workspaces)
	logins := service.NewDeviceLoginService(repository.NewDeviceLogins(database), tokens)
	return deviceHandlerTest{handler: NewDeviceHandler(logins, workspaces, &config.Config{}), logins: logins, workspaces: workspaces, owner: owner}
}

func deviceRequest(method, target string, form url.Values, user *model.User) *http.Request {
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
	test := newDeviceHandlerTest(t)
	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, "/device?code="+strings.ToLower(started.UserCode), nil, nil))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth" {
		t.Fatalf("guest: status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	cookie := deviceCookie(recorder)
	if cookie == nil || cookie.Value != service.NormalizeUserCode(started.UserCode) {
		t.Fatalf("pending device cookie = %#v", cookie)
	}

	// Any sign-in ends in redirectAfterAuth, which goes back to the page.
	auth := service.NewAuthService(nil, nil, nil, test.workspaces, nil, false, time.Hour, time.Hour, time.Hour, time.Hour, time.Hour)
	signIn := deviceRequest(http.MethodGet, "/auth/onboarding", nil, test.owner.User)
	signIn.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	NewAuthHandler(auth, nil, test.workspaces, &config.Config{}).OnboardingPage(recorder, signIn)
	want := "/device?code=" + started.UserCode
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != want {
		t.Fatalf("after sign-in: status %d, location %q, want %q", recorder.Code, recorder.Header().Get("Location"), want)
	}

	recorder = httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, want, nil, test.owner.User))
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "moin CLI on host-1") || !strings.Contains(body, started.UserCode) || !strings.Contains(body, "Approve") {
		t.Fatalf("signed in: status %d, body %s", recorder.Code, body)
	}
	if cookie := deviceCookie(recorder); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("the page did not clear the pending cookie: %#v", cookie)
	}
}

func TestDevicePageApproves(t *testing.T) {
	test := newDeviceHandlerTest(t)
	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"code": {started.UserCode}, "workspace_id": {test.owner.Workspace.ID}}
	recorder := httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "is logged in") {
		t.Fatalf("approve: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	token, raw, err := test.logins.Token(started.DeviceCode, service.Source{Via: service.ViaCLI})
	if err != nil || raw == "" || token.WorkspaceID != test.owner.Workspace.ID {
		t.Fatalf("Token() = %+v, %q, %v", token, raw, err)
	}

	// The code is used now: the page asks for a new one.
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "No login waits for this code") {
		t.Fatalf("second approve: status %d, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestDevicePageDenies(t *testing.T) {
	test := newDeviceHandlerTest(t)
	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	test.handler.Deny(recorder, deviceRequest(http.MethodPost, "/device/deny", url.Values{"code": {started.UserCode}}, test.owner.User))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Login denied") {
		t.Fatalf("deny: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if _, _, err := test.logins.Token(started.DeviceCode, service.Source{}); !errors.Is(err, service.ErrDeviceLoginDenied) {
		t.Fatalf("Token() after deny = %v", err)
	}
}

func TestDevicePageRefusesWrongCodesAndWorkspaces(t *testing.T) {
	test := newDeviceHandlerTest(t)
	recorder := httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, "/device?code=WRNG-CODE", nil, test.owner.User))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "No login waits for this code") || !strings.Contains(recorder.Body.String(), `aria-invalid="true"`) {
		t.Fatalf("wrong code: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	test.handler.Show(recorder, deviceRequest(http.MethodGet, "/device", nil, test.owner.User))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Enter the code your terminal shows") {
		t.Fatalf("no code: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	started, err := test.logins.Start("host-1")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"code": {started.UserCode}, "workspace_id": {"workspace-of-someone-else"}}
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/device/approve", form, test.owner.User))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "Choose one of your workspaces") {
		t.Fatalf("foreign workspace: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if _, err := test.logins.Pending(started.UserCode); err != nil {
		t.Fatalf("the login stays pending: %v", err)
	}
}
