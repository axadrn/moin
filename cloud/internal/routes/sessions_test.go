package routes

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// cliSignIn signs a CLI in through the browser, which is signed in, and
// returns its session token.
func cliSignIn(t *testing.T, server *httptest.Server, browser *http.Client) string {
	t.Helper()
	var login struct{ Secret, Code, URL string }
	postJSON(t, http.DefaultClient, server.URL+"/api/cli/login", "", nil, http.StatusOK, &login)
	if response, _ := browser.PostForm(server.URL+"/cli", url.Values{"code": {login.Code}}); response.StatusCode != http.StatusOK {
		t.Fatalf("allow: %s", response.Status)
	}
	var session struct{ Token string }
	postJSON(t, http.DefaultClient, server.URL+"/api/cli/login/finish", "", map[string]string{"secret": login.Secret}, http.StatusOK, &session)
	return session.Token
}

var cliRow = regexp.MustCompile(`(?s)>CLI</span>.*?name="session" value="([0-9a-f]{64})"`)

// The settings page lists the browsers and CLIs signed in, and signs one
// or all of them out. `moin watcher logout` signs its own CLI out.
func TestSessions(t *testing.T) {
	server, browser, mails := testSite(t)
	signIn(t, server, browser, mails, "ada@example.com")
	token := cliSignIn(t, server, browser)
	page := body(t, browser, server.URL+"/cloud/settings")
	if !strings.Contains(page, "This browser") || !strings.Contains(page, "Sign out everywhere") {
		t.Fatal("the settings page does not list the sessions")
	}
	id := cliRow.FindStringSubmatch(page)
	if id == nil {
		t.Fatalf("no CLI on the settings page")
	}
	response, _ := browser.PostForm(server.URL+"/cloud/sessions/end", url.Values{"session": {id[1]}})
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/cloud/settings" {
		t.Fatalf("sign the CLI out: %s at %s", response.Status, response.Request.URL)
	}
	getStatus(t, server.URL+"/api/cloud", token, http.StatusUnauthorized)
	if strings.Contains(body(t, browser, server.URL+"/cloud/settings"), ">CLI<") {
		t.Error("the CLI is still listed")
	}

	token = cliSignIn(t, server, browser)
	postJSON(t, http.DefaultClient, server.URL+"/api/cli/logout", token, nil, http.StatusNoContent, nil)
	getStatus(t, server.URL+"/api/cloud", token, http.StatusUnauthorized)
	postJSON(t, http.DefaultClient, server.URL+"/api/cli/logout", token, nil, http.StatusUnauthorized, nil)

	token = cliSignIn(t, server, browser)
	response, _ = browser.PostForm(server.URL+"/cloud/sessions/end", url.Values{"session": {"all"}})
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/" {
		t.Fatalf("sign out everywhere: %s at %s", response.Status, response.Request.URL)
	}
	getStatus(t, server.URL+"/api/cloud", token, http.StatusUnauthorized)
	if response, _ := browser.Get(server.URL + "/cloud/settings"); response.Request.URL.Path != "/login" {
		t.Errorf("the browser is still signed in: %s", response.Request.URL)
	}
}

// getStatus gets address with a session token and wants status.
func getStatus(t *testing.T, address, token string, status int) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, address, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != status {
		t.Errorf("GET %s: %s, want %d", address, response.Status, status)
	}
}
