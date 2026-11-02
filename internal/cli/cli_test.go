package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeAPI answers the paths a test gives and records every request.
type fakeAPI struct {
	mu       sync.Mutex
	requests []string
	answers  map[string]string
	status   int
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	parts := []string{key}
	for _, part := range []string{r.URL.RawQuery, string(body)} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	f.requests = append(f.requests, strings.Join(parts, " "))
	answer, ok := f.answers[key]
	status := f.status
	f.mu.Unlock()
	if status != 0 {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"Sign in to continue."}}`))
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"The app was not found."}}`))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/events") {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	_, _ = w.Write([]byte(answer))
}

func (f *fakeAPI) seen(want string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, request := range f.requests {
		if strings.HasPrefix(request, want) {
			return true
		}
	}
	return false
}

// runCLI runs one command against the fake API, without a terminal.
func runCLI(t *testing.T, api *fakeAPI, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPS_URL", server.URL)
	t.Setenv("OPS_TOKEN", "ops_test")
	t.Setenv("NO_COLOR", "1")
	var out, errOut strings.Builder
	code = Run(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestAppAsArgumentOrFlagAnywhere(t *testing.T) {
	env := `{"items":{"KEY":"secret"}}`
	for _, args := range [][]string{
		{"env", "shop", "--show"},
		{"env", "--show", "--app", "shop"},
		{"env", "--app", "shop", "--show"},
		{"env", "ls", "shop", "--show"},
	} {
		api := &fakeAPI{answers: map[string]string{"GET /api/v1/apps/shop/env": env}}
		code, stdout, stderr := runCLI(t, api, args...)
		if code != 0 || !strings.Contains(stdout, "KEY=secret") {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}

	api := &fakeAPI{answers: map[string]string{"GET /api/v1/apps/shop/logs": `{"lines":["web-1  | hello"]}`}}
	code, stdout, stderr := runCLI(t, api, "logs", "--app", "shop", "--tail", "5")
	if code != 0 || !strings.Contains(stdout, "hello") || !api.seen("GET /api/v1/apps/shop/logs tail=5") {
		t.Errorf("logs: code %d, stdout %q, stderr %q, requests %v", code, stdout, stderr, api.requests)
	}

	rollback := map[string]string{
		"POST /api/v1/apps/shop/rollback":        `{"number":4,"status":"running","from_number":3}`,
		"GET /api/v1/apps/shop/deploys/4/events": "event: done\ndata: {\"number\":4,\"status\":\"succeeded\"}\n\n",
	}
	for _, args := range [][]string{{"rollback", "v3", "--app", "shop", "--yes"}, {"rollback", "shop", "v3", "--yes"}, {"rollback", "3", "shop", "--yes"}} {
		api := &fakeAPI{answers: rollback}
		code, stdout, stderr := runCLI(t, api, args...)
		if code != 0 || !api.seen(`POST /api/v1/apps/shop/rollback {"number":3}`) {
			t.Errorf("%v: code %d, stdout %q, stderr %q, requests %v", args, code, stdout, stderr, api.requests)
		}
	}

	code, _, stderr = runCLI(t, &fakeAPI{}, "logs", "--app", "shop", "blog")
	if code != 1 || !strings.Contains(stderr, "Two apps") {
		t.Errorf("two apps: code %d, stderr %q", code, stderr)
	}
}

func TestOwnUsageNotGoFlags(t *testing.T) {
	code, stdout, stderr := runCLI(t, &fakeAPI{}, "logs", "--bogus")
	if code != 1 || !strings.Contains(stderr, "Unknown flag --bogus. Usage: `ops logs [--tail 200] [app]`") || strings.Contains(stdout+stderr, "Usage of") {
		t.Errorf("bad flag: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLI(t, &fakeAPI{}, "logs", "--help")
	if code != 0 || !strings.Contains(stdout, "ops logs [--tail 200] [app]") || !strings.Contains(stdout, "--tail") || strings.Contains(stdout+stderr, "Usage of") {
		t.Errorf("help: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, stdout, _ = runCLI(t, &fakeAPI{}, "login", "--help")
	if code != 0 || !strings.Contains(stdout, "stdin") || !strings.Contains(stdout, "(default http") {
		t.Errorf("login help: code %d, stdout %q", code, stdout)
	}
}

func TestDestructiveCommandsAsk(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{"DELETE /api/v1/servers/web-1": `{"uninstall_command":"ops agent uninstall"}`}}
	code, _, stderr := runCLI(t, api, "servers", "rm", "web-1")
	if code != 1 || !strings.Contains(stderr, "--yes") || api.seen("DELETE") {
		t.Errorf("without --yes: code %d, stderr %q, requests %v", code, stderr, api.requests)
	}
	code, stdout, stderr := runCLI(t, api, "servers", "rm", "web-1", "--yes")
	if code != 0 || !api.seen("DELETE /api/v1/servers/web-1") || !strings.Contains(stdout, "ops agent uninstall") {
		t.Errorf("with --yes: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, args := range [][]string{
		{"apps", "rm", "shop"},
		{"env", "rm", "--app", "shop", "KEY"},
		{"restore", "--app", "shop", "snap-1"},
		{"domains", "rm", "--app", "shop", "blog.de"},
		{"unlink", "shop"},
		{"hook", "shop"},
		{"backups", "bucket", "rm"},
		{"registry", "rm", "ghcr.io"},
		{"integrations", "rm", "tailscale"},
		{"sql", "--app", "shop", "--write", "DROP TABLE orders"},
	} {
		api := &fakeAPI{answers: map[string]string{"GET /api/v1/apps/shop": `{"app":{"name":"shop","domains":["blog.de"]}}`, "GET /api/v1/apps/shop/env": `{"items":{"KEY":"x"}}`, "GET /api/v1/backup-storage": `{"bucket":"ops-backups"}`}}
		code, _, stderr := runCLI(t, api, args...)
		if code != 1 || !strings.Contains(stderr, "--yes") {
			t.Errorf("%v: code %d, stderr %q", args, code, stderr)
		}
		for _, request := range api.requests {
			if !strings.HasPrefix(request, "GET ") {
				t.Errorf("%v changed something without --yes: %v", args, api.requests)
			}
		}
	}
}

func TestErrorsSayWhatToDo(t *testing.T) {
	code, _, stderr := runCLI(t, &fakeAPI{status: http.StatusUnauthorized}, "apps")
	if code != 1 || !strings.Contains(stderr, "OPS_TOKEN") || strings.Contains(stderr, "..") {
		t.Errorf("rejected token: code %d, stderr %q", code, stderr)
	}

	code, _, stderr = runCLI(t, &fakeAPI{}, "logs", "--app", "nope")
	if code != 1 || !strings.Contains(stderr, "There is no app nope; the name came from --app") {
		t.Errorf("unknown app from the flag: code %d, stderr %q", code, stderr)
	}

	dir := filepath.Join(t.TempDir(), "my-shop")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	code, _, stderr = runCLI(t, &fakeAPI{}, "logs")
	if code != 1 || !strings.Contains(stderr, "There is no app my-shop; the name came from the directory name") {
		t.Errorf("unknown app from the directory: code %d, stderr %q", code, stderr)
	}
}

func TestDeploys(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/apps/shop": `{"app":{"name":"shop","live_number":2},"deploys":[
			{"number":3,"status":"failed","error":"health check failed","created_at":"2026-11-01T10:00:00Z","finished_at":"2026-11-01T10:00:41Z"},
			{"number":2,"status":"succeeded","image":"ghcr.io/me/shop:2","created_at":"2026-11-01T09:00:00Z","finished_at":"2026-11-01T09:00:20Z"},
			{"number":1,"status":"succeeded","created_at":"2026-11-01T08:00:00Z"}]}`,
		"GET /api/v1/apps/shop/deploys/3": `{"number":3,"status":"failed","error":"health check failed","created_at":"2026-11-01T10:00:00Z","log":["→ building","step 1/3"]}`,
	}}
	code, stdout, stderr := runCLI(t, api, "deploys", "shop")
	if code != 0 || !strings.Contains(stdout, "v3") || !strings.Contains(stdout, "41s") || !strings.Contains(stdout, "health check failed") || !strings.Contains(stdout, "live") || !strings.Contains(stdout, "ghcr.io/me/shop:2") {
		t.Errorf("history: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLI(t, api, "deploys", "v3", "--app", "shop")
	if code != 0 || !strings.Contains(stdout, "building") || !strings.Contains(stdout, "step 1/3") {
		t.Errorf("one deploy: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// TestHelpRunsNothing runs every command of the usage with --help and -h:
// each prints its usage, exits with 0 and calls nothing, logged in or not.
func TestHelpRunsNothing(t *testing.T) {
	commands := [][]string{}
	for _, line := range strings.Split(usage, "\n") {
		name, _, ok := strings.Cut(strings.TrimPrefix(line, "  "), "  ")
		if !strings.HasPrefix(line, "  ") || !ok || strings.HasPrefix(name, "ops") {
			continue
		}
		words := []string{}
		for _, word := range strings.Fields(name) {
			if !strings.HasPrefix(word, "-") {
				words = append(words, word)
			}
		}
		commands = append(commands, words)
	}
	if len(commands) < 50 {
		t.Fatalf("found only %d commands in the usage", len(commands))
	}
	for _, words := range commands {
		for _, help := range []string{"--help", "-h"} {
			args := append(slices.Clone(words), help)
			for _, loggedIn := range []bool{true, false} {
				api := &fakeAPI{}
				server := httptest.NewServer(api)
				config := filepath.Join(t.TempDir(), "ops", "config.json")
				if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, []byte(`{"url":"http://127.0.0.1:1"}`), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", t.TempDir())
				t.Setenv("XDG_CONFIG_HOME", filepath.Dir(filepath.Dir(config)))
				t.Setenv("NO_COLOR", "1")
				t.Setenv("OPS_URL", "")
				t.Setenv("OPS_TOKEN", "")
				if loggedIn {
					t.Setenv("OPS_URL", server.URL)
					t.Setenv("OPS_TOKEN", "ops_test")
				}
				var stdout, stderr strings.Builder
				code := Run(args, strings.NewReader(""), &stdout, &stderr)
				server.Close()
				if code != 0 || !strings.Contains(stdout.String(), "Usage") || (words[0] != "help" && !strings.Contains(stdout.String(), "ops "+words[0])) || stderr.Len() > 0 || len(api.requests) > 0 {
					t.Errorf("%v (logged in %v): code %d, stdout %q, stderr %q, requests %v", args, loggedIn, code, stdout.String(), stderr.String(), api.requests)
				}
				if _, err := os.Stat(config); err != nil {
					t.Errorf("%v removed the config: %v", args, err)
				}
			}
		}
	}
}

func TestHelpListsOnlyItsFlags(t *testing.T) {
	code, stdout, _ := runCLI(t, &fakeAPI{}, "backups", "bucket", "rm", "--help")
	if code != 0 || !strings.Contains(stdout, "--yes") || strings.Contains(stdout, "--endpoint") || strings.Contains(stdout, "--app") {
		t.Errorf("bucket rm help: code %d, stdout %q", code, stdout)
	}
	code, stdout, _ = runCLI(t, &fakeAPI{}, "backups", "bucket", "set", "--help")
	if code != 0 || !strings.Contains(stdout, "--access-key") || strings.Contains(stdout, "--yes") {
		t.Errorf("bucket set help: code %d, stdout %q", code, stdout)
	}
}

func TestSQLWrite(t *testing.T) {
	query := map[string]string{"POST /api/v1/apps/shop/databases/query": `{"changes":2}`}
	api := &fakeAPI{answers: query}
	code, stdout, stderr := runCLI(t, api, "sql", "--app", "shop", "--write", "--yes", "DELETE FROM orders")
	if code != 0 || !strings.Contains(stdout, "2 rows changed") {
		t.Errorf("--yes: code %d, stdout %q, stderr %q, requests %v", code, stdout, stderr, api.requests)
	}
	api = &fakeAPI{answers: query}
	code, stdout, stderr = runCLI(t, api, "sql", "--app", "shop", "--write", "UPDATE orders SET paid = 1")
	if code != 0 || !strings.Contains(stdout, "2 rows changed") {
		t.Errorf("update asks nothing: code %d, stdout %q, stderr %q, requests %v", code, stdout, stderr, api.requests)
	}
}

func TestEnvPullAsksBeforeReplacing(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("LOCAL=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{answers: map[string]string{"GET /api/v1/apps/shop/env": `{"items":{"KEY":"secret"}}`}}
	code, _, stderr := runCLI(t, api, "env", "pull", "--app", "shop")
	body, _ := os.ReadFile(".env")
	if code != 1 || !strings.Contains(stderr, "--yes") || string(body) != "LOCAL=1\n" {
		t.Errorf("without --yes: code %d, stderr %q, .env %q", code, stderr, body)
	}
	code, _, stderr = runCLI(t, api, "env", "pull", "--app", "shop", "--yes")
	body, _ = os.ReadFile(".env")
	if code != 0 || string(body) != "KEY=secret\n" {
		t.Errorf("with --yes: code %d, stderr %q, .env %q", code, stderr, body)
	}
}

func TestLinkUnreadableRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_failed","message":"Check the highlighted fields and try again.","fields":{"repo":"ops cannot read this repo. Install the ops GitHub App on its account and give it the repo."}}}`))
	}))
	defer server.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPS_URL", server.URL)
	t.Setenv("OPS_TOKEN", "ops_test")
	t.Setenv("NO_COLOR", "1")
	var stdout, stderr strings.Builder
	code := Run([]string{"link", "me/private", "--app", "shop"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "cannot read me/private") || !strings.Contains(stderr.String(), "ops github") || strings.Contains(stderr.String(), "highlighted") {
		t.Errorf("code %d, stderr %q", code, stderr.String())
	}
}

// TestNothingChanged: a change the app had already says so and follows no
// restart.
func TestNothingChanged(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{"GET /api/v1/apps/shop": `{"app":{"name":"shop","protected":false},"deploys":[]}`}}
	code, stdout, stderr := runCLI(t, api, "protect", "off", "shop")
	if code != 0 || !strings.Contains(stdout, "Nothing changed") || strings.Contains(stdout, "public again") || api.seen("DELETE") {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// TestAsksFirst: commands that remove or replace something need a terminal
// or --yes, and change nothing without them.
func TestAsksFirst(t *testing.T) {
	answers := map[string]string{
		"GET /api/v1/apps/shop":                         `{"app":{"name":"shop","protected":true,"hook":true},"deploys":[]}`,
		"GET /api/v1/servers":                           `{"items":[{"id":"s1","name":"web-1"}]}`,
		"GET /api/v1/workspaces":                        `{"items":[{"workspace":{"slug":"acme"}}]}`,
		"GET /api/v1/workspaces/acme":                   `{"workspace":{"name":"Acme","slug":"acme"},"invitations":[{"id":"i1","email":"ada@example.com","role":"member"}]}`,
		"DELETE /api/v1/apps/shop/protection":           `{"app":{"name":"shop"},"deploy":null,"unchanged":true}`,
		"DELETE /api/v1/apps/shop/hook":                 ``,
		"DELETE /api/v1/workspaces/acme/invitations/i1": ``,
		"PUT /api/v1/servers/s1/security":               `{"id":"s1","name":"web-1","firewall":true}`,
	}
	for _, args := range [][]string{
		{"protect", "off", "shop"},
		{"hook", "off", "shop"},
		{"rollback", "shop"},
		{"team", "revoke", "ada@example.com"},
		{"servers", "secure", "web-1", "--firewall", "on"},
	} {
		api := &fakeAPI{answers: answers}
		code, _, stderr := runCLI(t, api, args...)
		if code != 1 || !strings.Contains(stderr, "--yes") || api.seen("DELETE") || api.seen("PUT") || api.seen("POST") {
			t.Errorf("%v without --yes: code %d, stderr %q, requests %v", args, code, stderr, api.requests)
		}
		api = &fakeAPI{answers: answers}
		code, _, stderr = runCLI(t, api, append(args[:len(args):len(args)], "--yes")...)
		if args[0] != "rollback" && (code != 0 || !(api.seen("DELETE") || api.seen("PUT"))) {
			t.Errorf("%v --yes: code %d, stderr %q, requests %v", args, code, stderr, api.requests)
		}
	}
}

// TestEnvRmMissing: a key the app does not have is not removed and says so.
func TestEnvRmMissing(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{"GET /api/v1/apps/shop/env": `{"items":{"KEY":"x"}}`, "PATCH /api/v1/apps/shop/env": `{"items":{}}`}}
	code, stdout, stderr := runCLI(t, api, "env", "rm", "--app", "shop", "NOT_THERE", "--yes")
	if code != 0 || !strings.Contains(stdout, "shop has no NOT_THERE") || strings.Contains(stdout, "Removed") || api.seen("PATCH") {
		t.Errorf("code %d, stdout %q, stderr %q, requests %v", code, stdout, stderr, api.requests)
	}
	code, stdout, _ = runCLI(t, api, "env", "rm", "--app", "shop", "NOT_THERE", "KEY", "--yes")
	if code != 0 || !strings.Contains(stdout, "Removed KEY from") || !api.seen(`PATCH /api/v1/apps/shop/env {"set":null,"unset":["KEY"]}`) {
		t.Errorf("code %d, stdout %q, requests %v", code, stdout, api.requests)
	}
}

// TestLoginRefusesPlainHTTP: login never sends a token over plain http
// to a host on the internet.
func TestLoginRefusesPlainHTTP(t *testing.T) {
	api := &fakeAPI{}
	code, _, stderr := runCLI(t, api, "login", "--url", "http://ops.example.com", "--token", "ops_test")
	if code == 0 || !strings.Contains(stderr, "https://ops.example.com") || len(api.requests) > 0 {
		t.Fatalf("code %d, stderr %q, requests %v", code, stderr, api.requests)
	}
}
