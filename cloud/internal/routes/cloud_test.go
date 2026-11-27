package routes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/cloud/internal/github"
	"github.com/axadrn/moin/cloud/internal/handler"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/watchers"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

// outbox keeps the mails a test sends.
type outbox struct {
	mu   sync.Mutex
	sent []mailer.Message
}

func (o *outbox) Send(_ context.Context, m mailer.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, m)
	return nil
}

func (o *outbox) last(t *testing.T) mailer.Message {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.sent) == 0 {
		t.Fatal("no mail sent")
	}
	return o.sent[len(o.sent)-1]
}

// testCloud is moin cloud on a fresh database, sending mail to mails.
func testCloud(t *testing.T, appURL string, mails *outbox) *handler.Cloud {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store := workspace.NewStore(database)
	var send mailer.Mailer = mailer.Log{}
	if mails != nil {
		send = mails
	}
	return handler.NewCloud(store, watchers.New(store, send, appURL), send, github.New("", "", "", ""), appURL, false)
}

// testSite is the site with moin cloud, served on a test server, and a
// browser with cookies.
func testSite(t *testing.T) (*httptest.Server, *http.Client, *outbox) {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	appURL := "http://" + server.Listener.Addr().String()
	mails := &outbox{}
	site, err := New(&config.Config{RenderConfig: config.RenderConfig{AppURL: appURL}}, testCloud(t, appURL, mails))
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = site
	server.Start()
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	return server, &http.Client{Jar: jar}, mails
}

var linkInMail = regexp.MustCompile(`http://\S+/login/link\?\S+`)

// signIn signs the browser in by a mail link and returns the mail's link.
func signIn(t *testing.T, server *httptest.Server, browser *http.Client, mails *outbox, mail string) {
	t.Helper()
	response, err := browser.PostForm(server.URL+"/login", url.Values{"mail": {mail}})
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("send the link: %v %v", err, response.Status)
	}
	link := linkInMail.FindString(mails.last(t).Text)
	parsed, _ := url.Parse(link)
	// Opening the link only shows a button.
	response, err = browser.Get(link)
	if err != nil || response.StatusCode != http.StatusOK || len(browser.Jar.Cookies(parsed)) != 0 {
		t.Fatalf("open the link: %v %v, cookies %v", err, response.Status, browser.Jar.Cookies(parsed))
	}
	response, err = browser.PostForm(server.URL+"/login/link", url.Values{"token": {parsed.Query().Get("token")}, "next": {"/cloud"}})
	if err != nil || response.StatusCode != http.StatusOK || response.Request.URL.Path != "/cloud" {
		t.Fatalf("use the link: %v %v at %v", err, response.Status, response.Request.URL)
	}
}

// A mail link signs in once, into the workspace of the mail.
func TestMailLinkSignIn(t *testing.T) {
	server, browser, mails := testSite(t)
	response, _ := browser.Get(server.URL + "/cloud")
	if response.Request.URL.Path != "/login" {
		t.Fatalf("/cloud without a session went to %s", response.Request.URL)
	}
	signIn(t, server, browser, mails, " Ada@Example.com ")
	if mails.last(t).To != "ada@example.com" {
		t.Errorf("mail to %q", mails.last(t).To)
	}
	page := body(t, browser, server.URL+"/cloud")
	if !strings.Contains(page, "ada@example.com") || !strings.Contains(page, "No servers yet") {
		t.Errorf("workspace page: %s", page)
	}
	// The same link again does not work.
	token := mustQuery(t, linkInMail.FindString(mails.last(t).Text), "token")
	response, _ = http.PostForm(server.URL+"/login/link", url.Values{"token": {token}})
	if response.StatusCode != http.StatusGone {
		t.Errorf("a used link: %s", response.Status)
	}
	// Another site's form does not sign anyone in.
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/login", strings.NewReader("mail=ada%40example.com"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://evil.example.com")
	if response, _ := http.DefaultClient.Do(request); response.StatusCode != http.StatusForbidden {
		t.Errorf("a form of another site: %s", response.Status)
	}
}

// Three links per mail, then a pause.
func TestMailLinkLimit(t *testing.T) {
	server, browser, _ := testSite(t)
	for i := range 4 {
		response, _ := browser.PostForm(server.URL+"/login", url.Values{"mail": {"ada@example.com"}})
		want := http.StatusOK
		if i == 3 {
			want = http.StatusTooManyRequests
		}
		if response.StatusCode != want {
			t.Errorf("link %d: %s", i+1, response.Status)
		}
	}
}

// moin cloud login, connect, a report, and the page that shows it.
func TestCLILoginConnectReport(t *testing.T) {
	server, browser, mails := testSite(t)
	cli := http.DefaultClient

	var login struct{ Secret, Code, URL string }
	postJSON(t, cli, server.URL+"/api/cli/login", "", nil, http.StatusOK, &login)
	if !strings.HasPrefix(login.URL, server.URL+"/cli?code=") || len(login.Code) != 9 {
		t.Fatalf("login = %+v", login)
	}
	postJSON(t, cli, server.URL+"/api/cli/login/finish", "", map[string]string{"secret": login.Secret}, http.StatusAccepted, nil)

	// The browser signs in on the way and shows the same code.
	response, _ := browser.Get(login.URL)
	if response.Request.URL.Path != "/login" || response.Request.URL.Query().Get("next") != "/cli?code="+login.Code {
		t.Fatalf("not signed in, /cli went to %s", response.Request.URL)
	}
	signIn(t, server, browser, mails, "ada@example.com")
	if page := body(t, browser, login.URL); !strings.Contains(page, login.Code) {
		t.Fatalf("the page does not show the code")
	}
	response, _ = browser.PostForm(server.URL+"/cli", url.Values{"code": {login.Code}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("allow: %s", response.Status)
	}
	var session struct{ Token string }
	postJSON(t, cli, server.URL+"/api/cli/login/finish", "", map[string]string{"secret": login.Secret}, http.StatusOK, &session)
	postJSON(t, cli, server.URL+"/api/cli/login/finish", "", map[string]string{"secret": login.Secret}, http.StatusGone, nil)

	var connected struct{ URL, Token string }
	postJSON(t, cli, server.URL+"/api/cloud/servers", session.Token, map[string]string{"name": "prod"}, http.StatusOK, &connected)
	if connected.URL != server.URL || connected.Token == "" {
		t.Fatalf("connect = %+v", connected)
	}
	postJSON(t, cli, server.URL+"/api/cloud/servers", "wrong", map[string]string{"name": "prod"}, http.StatusUnauthorized, nil)

	good := map[string]any{"server": "prod", "version": "v1.0.0", "every_seconds": 60, "apps": []map[string]any{{"name": "shop", "release": 3, "running": true}}}
	postJSON(t, cli, server.URL+"/report", "wrong-token", good, http.StatusUnauthorized, nil)
	postJSON(t, cli, server.URL+"/report", connected.Token, map[string]any{"server": "web-1"}, http.StatusBadRequest, nil)
	postJSON(t, cli, server.URL+"/report", connected.Token, good, http.StatusNoContent, nil)

	var space struct {
		Mail, Plan string
		Servers    []struct {
			Name string
			OK   bool
			Apps int
		}
	}
	getJSON(t, server.URL+"/api/cloud", session.Token, &space)
	if space.Mail != "ada@example.com" || !strings.HasPrefix(space.Plan, "Trial until") || len(space.Servers) != 1 || space.Servers[0].Apps != 1 || !space.Servers[0].OK {
		t.Errorf("moin cloud = %+v", space)
	}
	if page := body(t, browser, server.URL+"/cloud"); !strings.Contains(page, "shop") || !strings.Contains(page, "v1.0.0") {
		t.Errorf("the page does not show the report")
	}

	request, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/cloud/servers/prod", nil)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	if response, _ := cli.Do(request); response.StatusCode != http.StatusNoContent {
		t.Errorf("disconnect: %s", response.Status)
	}
	postJSON(t, cli, server.URL+"/report", connected.Token, good, http.StatusUnauthorized, nil)
}

func postJSON(t *testing.T, client *http.Client, address, token string, value any, want int, out any) {
	t.Helper()
	data, _ := json.Marshal(value)
	request, _ := http.NewRequest(http.MethodPost, address, strings.NewReader(string(data)))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("POST %s = %s, want %d", address, response.Status, want)
	}
	if out != nil {
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func getJSON(t *testing.T, address, token string, out any) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, address, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %v %v", address, err, response.Status)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func body(t *testing.T, client *http.Client, address string) string {
	t.Helper()
	response, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	text, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

func mustQuery(t *testing.T, link, key string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get(key)
}
