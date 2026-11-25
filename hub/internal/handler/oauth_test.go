package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
)

type oauthHandlerTest struct {
	deviceHandlerTest
	handler *OAuthHandler
	oauth   *service.OAuthService
	client  *model.OAuthClient
}

// newOAuthHandlerTest is the approval page of a hub for AI agents, with a
// client that registered itself under a name that tries to look like more.
func newOAuthHandlerTest(t *testing.T, passkeys bool) oauthHandlerTest {
	t.Helper()
	device := newDeviceHandlerTest(t, passkeys)
	oauth := service.NewOAuthService(repository.NewOAuth(device.db), device.connections, device.passkeys, "https://hub.example.com")
	client, err := oauth.Register(service.NewClient{ClientName: `Claude <b>official</b>`, RedirectURIs: []string{"https://claude.example.com/callback", "http://localhost/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	return oauthHandlerTest{
		deviceHandlerTest: device,
		handler:           NewOAuthHandler(oauth, device.workspaces, device.passkeys, device.auth, &config.Config{}),
		oauth:             oauth,
		client:            client,
	}
}

const oauthTestVerifier = "the-verifier-of-the-handler-test-long-enough-for-pkce"

// query is a good authorization request to redirect.
func (test oauthHandlerTest) query(redirect string) url.Values {
	sum := sha256.Sum256([]byte(oauthTestVerifier))
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {test.client.ID},
		"redirect_uri":          {redirect},
		"state":                 {"state-1"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {"https://hub.example.com/mcp"},
	}
}

func approvalCookie(recorder *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == pendingApprovalCookie {
			return cookie
		}
	}
	return nil
}

// TestOAuthPageSignsInFirstAndShowsTheClientAsUntrusted: a guest signs in
// and comes back to the same request. The page then shows the client's
// name as text, the host it goes back to, the rights and Allow.
func TestOAuthPageSignsInFirstAndShowsTheClientAsUntrusted(t *testing.T) {
	test := newOAuthHandlerTest(t, false)
	query := test.query("https://claude.example.com/callback")
	recorder := httptest.NewRecorder()
	test.handler.Authorize(recorder, deviceRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil, nil, nil))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth" {
		t.Fatalf("guest: status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	cookie := approvalCookie(recorder)
	if cookie == nil {
		t.Fatal("no pending cookie")
	}
	signIn := deviceRequest(http.MethodGet, "/auth/onboarding", nil, test.owner.User, nil)
	signIn.AddCookie(cookie)
	back, ok := pendingApproval(signIn)
	if !ok || back != "/oauth/authorize?"+query.Encode() {
		t.Fatalf("after sign-in = %q, %v", back, ok)
	}

	recorder = httptest.NewRecorder()
	test.handler.Authorize(recorder, deviceRequest(http.MethodGet, back, nil, test.owner.User, test.session(t, 0)))
	body := recorder.Body.String()
	for _, part := range []string{"Claude &lt;b&gt;official&lt;/b&gt;", "It will send you back to claude.example.com.", "Read: apps, logs, status", `name="rights" value="deploy" checked`, `name="client_id" value="` + test.client.ID + `"`, `formaction="/oauth/deny"`, ">Allow<"} {
		if !strings.Contains(body, part) {
			t.Fatalf("the page misses %q: status %d, body %s", part, recorder.Code, body)
		}
	}
	if strings.Contains(body, "<b>official") {
		t.Fatal("the client's name is not escaped")
	}

	// A client on this computer: the page says only its word names it.
	recorder = httptest.NewRecorder()
	test.handler.Authorize(recorder, deviceRequest(http.MethodGet, "/oauth/authorize?"+test.query("http://localhost:5555/callback").Encode(), nil, test.owner.User, test.session(t, 0)))
	if !strings.Contains(recorder.Body.String(), "an app on this computer") {
		t.Fatalf("loopback: body %s", recorder.Body.String())
	}
}

// TestOAuthPageRefusesBadRequests: an unknown client or a redirect URI it
// did not register stays here; other mistakes go back to the client.
func TestOAuthPageRefusesBadRequests(t *testing.T) {
	test := newOAuthHandlerTest(t, false)
	for name, change := range map[string]func(url.Values){
		"unknown client": func(q url.Values) { q.Set("client_id", "nope") },
		"wrong redirect": func(q url.Values) { q.Set("redirect_uri", "https://evil.example.com/callback") },
		"no redirect":    func(q url.Values) { q.Del("redirect_uri") },
		"http elsewhere": func(q url.Values) { q.Set("redirect_uri", "http://claude.example.com/callback") },
	} {
		query := test.query("https://claude.example.com/callback")
		change(query)
		recorder := httptest.NewRecorder()
		test.handler.Authorize(recorder, deviceRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil, test.owner.User, test.session(t, 0)))
		if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Location") != "" || !strings.Contains(recorder.Body.String(), "Nothing got access") && !strings.Contains(recorder.Body.String(), "not registered") {
			t.Errorf("%s: status %d, location %q", name, recorder.Code, recorder.Header().Get("Location"))
		}
	}
	// Any other mistake goes back to the client, but only with a click:
	// the hub never sends anyone on by itself from a link it did not
	// approve, or it would be an open redirect.
	for name, change := range map[string]func(url.Values){
		"plain PKCE":     func(q url.Values) { q.Set("code_challenge_method", "plain") },
		"repeated state": func(q url.Values) { q.Add("state", "other") },
	} {
		query := test.query("https://claude.example.com/callback")
		change(query)
		recorder := httptest.NewRecorder()
		test.handler.Authorize(recorder, deviceRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil, nil, nil))
		body := recorder.Body.String()
		if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Location") != "" || recorder.Header().Get("Refresh") != "" || !strings.Contains(body, "Back to claude.example.com") || !strings.Contains(body, "error=invalid_request") || !strings.Contains(body, "state=state-1") {
			t.Fatalf("%s: status %d, headers %v, body %s", name, recorder.Code, recorder.Header(), body)
		}
	}
	// A client_id sent twice is not one client.
	query := test.query("https://claude.example.com/callback")
	query.Add("client_id", "other")
	recorder := httptest.NewRecorder()
	test.handler.Authorize(recorder, deviceRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil, nil, nil))
	if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), "Back to") {
		t.Fatalf("repeated client_id: status %d, body %s", recorder.Code, recorder.Body.String())
	}
}

// TestOAuthPageApprovesAndSendsBack: Allow with a fresh sign-in makes the
// connection and sends the person back with the code, which gives its
// tokens; an old sign-in allows nothing; Cancel sends access_denied.
func TestOAuthPageApprovesAndSendsBack(t *testing.T) {
	test := newOAuthHandlerTest(t, false)
	redirect := "https://claude.example.com/callback"
	form := test.query(redirect)
	form.Set("workspace_id", test.owner.Workspace.ID)
	form["rights"] = []string{"deploy"}

	old := test.session(t, service.FreshSignIn+time.Minute)
	recorder := httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/oauth/approve", form, test.owner.User, old))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "older than five minutes") || recorder.Header().Get("Refresh") != "" {
		t.Fatalf("old sign-in: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/oauth/approve", form, test.owner.User, test.session(t, time.Minute)))
	refresh := recorder.Header().Get("Refresh")
	if recorder.Code != http.StatusOK || !strings.HasPrefix(refresh, "0; url="+redirect+"?") || !strings.Contains(recorder.Body.String(), "Back to claude.example.com") {
		t.Fatalf("approve: status %d, Refresh %q, body %s", recorder.Code, refresh, recorder.Body.String())
	}
	back, err := url.Parse(strings.TrimPrefix(refresh, "0; url="))
	if err != nil || back.Query().Get("state") != "state-1" || back.Query().Get("iss") != "https://hub.example.com" {
		t.Fatalf("sent back to %s", back)
	}
	tokens, err := test.oauth.Exchange(service.CodeExchange{Code: back.Query().Get("code"), Verifier: oauthTestVerifier, RedirectURI: redirect, ClientID: test.client.ID})
	if err != nil || tokens.Connection.Rights != model.RightRead|model.RightDeploy || tokens.Connection.ClientName != `Claude <b>official</b>` || tokens.Connection.Label() != `Claude <b>official</b> · claude.example.com` {
		t.Fatalf("Exchange() = %+v, %v", tokens, err)
	}

	recorder = httptest.NewRecorder()
	test.handler.Deny(recorder, deviceRequest(http.MethodPost, "/oauth/deny", test.query(redirect), test.owner.User, test.session(t, 0)))
	denied, _ := url.Parse(strings.TrimPrefix(recorder.Header().Get("Refresh"), "0; url="))
	if recorder.Code != http.StatusOK || denied.Query().Get("error") != "access_denied" || denied.Query().Get("code") != "" || !strings.Contains(recorder.Body.String(), "Nothing got access") {
		t.Fatalf("deny: status %d, sent to %s", recorder.Code, denied)
	}

	// Sign in again: back to the same request after.
	recorder = httptest.NewRecorder()
	test.handler.Reauth(recorder, deviceRequest(http.MethodPost, "/oauth/reauth", form, test.owner.User, old))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth" || approvalCookie(recorder) == nil {
		t.Fatalf("reauth: status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

// TestOAuthPageNeedsThePasskey: an account with a passkey allows only with
// it, as for `moin login`.
func TestOAuthPageNeedsThePasskey(t *testing.T) {
	test := newOAuthHandlerTest(t, true)
	fresh := test.session(t, 0)
	if err := repository.NewPasskeys(test.db).Create(&model.Passkey{ID: "p1", UserID: test.owner.User.ID, CredentialID: []byte{1}, Credential: "{}", Name: "Passkey", CreatedAt: time.Now()}, fresh.ID); err != nil {
		t.Fatal(err)
	}
	query := test.query("https://claude.example.com/callback")
	recorder := httptest.NewRecorder()
	test.handler.Authorize(recorder, deviceRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil, test.owner.User, fresh))
	if body := recorder.Body.String(); !strings.Contains(body, "Allow with passkey") || !strings.Contains(body, `data-passkey-url="/oauth/passkey"`) {
		t.Fatalf("with a passkey: body %s", body)
	}
	query.Set("workspace_id", test.owner.Workspace.ID)
	recorder = httptest.NewRecorder()
	test.handler.Approve(recorder, deviceRequest(http.MethodPost, "/oauth/approve", query, test.owner.User, fresh))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "Allow with your passkey.") {
		t.Fatalf("approve without the passkey: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if connections, _ := test.connections.List(test.owner.User.ID); len(connections) != 0 {
		t.Fatalf("a connection without the passkey: %+v", connections)
	}
}

// TestPendingApprovalLeadsOnlyToApprovalPages: the cookie never sends
// anyone off the hub or elsewhere on it.
func TestPendingApprovalLeadsOnlyToApprovalPages(t *testing.T) {
	for raw, want := range map[string]string{
		"/device?code=ABCD-EFGH":          "/device?code=ABCD-EFGH",
		"/oauth/authorize?client_id=x":    "/oauth/authorize?client_id=x",
		"https://evil.example.com/device": "",
		"//evil.example.com/device":       "",
		"/app/settings":                   "",
		"/oauth/authorize/../../app":      "",
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.AddCookie(&http.Cookie{Name: pendingApprovalCookie, Value: base64.RawURLEncoding.EncodeToString([]byte(raw))})
		got, ok := pendingApproval(request)
		if got != want || ok != (want != "") {
			t.Errorf("pendingApproval(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
}
