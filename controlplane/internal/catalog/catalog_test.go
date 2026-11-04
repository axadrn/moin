package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestEntries: every entry in content/catalog loads and is valid, so a
// broken one fails here, not on a customer's install.
func TestEntries(t *testing.T) {
	entries, err := Load(filepath.Join("..", "..", "content", "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 8 {
		t.Fatalf("%d entries", len(entries))
	}
	for _, entry := range entries {
		if !strings.Contains(string(entry.Compose), "x-ops:") {
			t.Errorf("%s: compose.yml has no x-ops block", entry.Slug)
		}
		// pgvector/pgvector is Postgres with vectors; the agent backs it up
		// like postgres.
		runsPostgres := strings.Contains(string(entry.Compose), "image: postgres:") || strings.Contains(string(entry.Compose), "image: pgvector/pgvector:")
		if entry.Database == "postgres" && !runsPostgres {
			t.Errorf("%s: says postgres but runs none", entry.Slug)
		}
		if runsPostgres && (!strings.Contains(string(entry.Compose), "pg_isready") || !strings.Contains(string(entry.Compose), "condition: service_healthy")) {
			t.Errorf("%s: its Postgres needs a pg_isready health check the app waits for", entry.Slug)
		}
		if strings.Contains(string(entry.Compose), "PASSWORD: ") && !strings.Contains(string(entry.Compose), "PASSWORD: ${") {
			t.Errorf("%s: a password is written out", entry.Slug)
		}
		if strings.TrimSpace(string(entry.Body)) == "" {
			t.Errorf("%s: no description", entry.Slug)
		}
	}
}

func TestLoadRefusesBadEntries(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no version":   {"app.md": "---\nname: A\npitch: p\ncategory: Analytics\nwebsite: https://a.example\nimage: a/b\nport: 80\n---\nx", "compose.yml": "services:\n  web:\n    image: a/b:1.0.0\n"},
		"bad category": {"app.md": "---\nname: A\npitch: p\ncategory: Games\nwebsite: https://a.example\nimage: a/b\nport: 80\n---\nx", "compose.yml": "services:\n  web:\n    image: a/b:${VERSION}\n"},
		"no port":      {"app.md": "---\nname: A\npitch: p\ncategory: Analytics\nwebsite: https://a.example\nimage: a/b\n---\nx", "compose.yml": "services:\n  web:\n    image: a/b:${VERSION}\n"},
	} {
		dir := t.TempDir()
		entry := filepath.Join(dir, "a")
		if err := os.MkdirAll(entry, 0o755); err != nil {
			t.Fatal(err)
		}
		for file, body := range files {
			if err := os.WriteFile(filepath.Join(entry, file), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Load(dir); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestSecrets(t *testing.T) {
	compose := []byte("image: a:${VERSION}\nA: ${DB_PASSWORD}\nB: ${APP_SECRET}\nC: ${DB_PASSWORD}\nD: ${OPTIONAL:-x}\nE: $PLAIN\n")
	if got := Secrets(compose); !slices.Equal(got, []string{"APP_SECRET", "DB_PASSWORD"}) {
		t.Errorf("Secrets = %v", got)
	}
}

func TestReleases(t *testing.T) {
	tags := []Version{{Tag: "latest"}, {Tag: "1.9.2"}, {Tag: "1.10.0"}, {Tag: "nightly"}, {Tag: "1.10.0-rc.1"}, {Tag: "sha-abc1234"}, {Tag: "v2.0.0"}, {Tag: "2.0"}, {Tag: "1.10.0"}, {Tag: "2.46.5-e78ad8c"}}
	var got []string
	for _, release := range Releases(tags, nil) {
		got = append(got, release.Tag)
	}
	if want := []string{"v2.0.0", "1.10.0", "1.9.2"}; !slices.Equal(got, want) {
		t.Errorf("Releases = %v, want %v", got, want)
	}
	if Compare("v1.2.10", "1.2.9") <= 0 || Compare("2026.10.1", "2026.9.30") <= 0 || Compare("1.0.0", "1.0.0") != 0 {
		t.Error("Compare orders wrong")
	}
}

// TestUpToChannel: n8n tags its betas like releases; stable names 2.46.5,
// so 2.47.0 and 3.1.0 stay out.
func TestUpToChannel(t *testing.T) {
	tags := []Version{{Tag: "3.1.0", Digest: "c"}, {Tag: "2.47.0", Digest: "b"}, {Tag: "next", Digest: "b"}, {Tag: "2.46.5", Digest: "a"}, {Tag: "stable", Digest: "a"}, {Tag: "2.46.4", Digest: "z"}}
	releases := Releases(tags, nil)
	var got []string
	for _, release := range UpToChannel(releases, tags, "stable") {
		got = append(got, release.Tag)
	}
	if want := []string{"2.46.5", "2.46.4"}; !slices.Equal(got, want) {
		t.Errorf("UpToChannel = %v, want %v", got, want)
	}
	if len(UpToChannel(releases, tags, "")) != 4 || len(UpToChannel(releases, tags, "missing")) != 4 {
		t.Error("without a channel the releases must stay")
	}
}

func TestRegistryHub(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/repositories/library/redis/tags" {
			http.NotFound(w, r)
			return
		}
		page := map[string]any{"results": []map[string]any{{"name": "7.4.1", "last_updated": "2026-10-01T10:00:00Z"}, {"name": "latest"}}}
		if r.URL.Query().Get("page") == "" {
			page["next"] = server.URL + r.URL.Path + "?page=2"
		} else {
			page["results"] = []map[string]any{{"name": "7.2.0", "tag_last_pushed": "2026-01-01T10:00:00Z"}}
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer server.Close()
	registry := Registry{Client: server.Client(), HubURL: server.URL, GHCRURL: server.URL}
	tags, err := registry.Tags(context.Background(), "redis")
	if err != nil {
		t.Fatal(err)
	}
	releases := Releases(tags, nil)
	if len(releases) != 2 || releases[0].Tag != "7.4.1" || releases[0].Date.Year() != 2026 || releases[1].Tag != "7.2.0" {
		t.Errorf("releases = %+v", releases)
	}
}

func TestRegistryGHCR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.URL.Query().Get("scope") != "repository:me/app:pull" {
				http.Error(w, "scope", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"token":"t0k"}`))
		case "/v2/me/app/tags/list":
			if r.Header.Get("Authorization") != "Bearer t0k" {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"tags":["1.0.0","1.1.0","latest","sha256-abc.sig"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	registry := Registry{Client: server.Client(), HubURL: server.URL, GHCRURL: server.URL}
	tags, err := registry.Tags(context.Background(), "ghcr.io/me/app")
	if err != nil {
		t.Fatal(err)
	}
	if releases := Releases(tags, nil); len(releases) != 2 || releases[0].Tag != "1.1.0" {
		t.Errorf("releases = %+v", releases)
	}
	if _, err := registry.Tags(context.Background(), "quay.io/me/app"); err == nil {
		t.Error("quay.io read")
	}
}

func TestStoreKeepsReleasesWhenTheRegistryFails(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"name":"1.0.0"}]}`))
	}))
	defer server.Close()
	store := NewStore([]Entry{{Slug: "a", Image: "me/a", Filter: defaultFilter}}, Registry{Client: server.Client(), HubURL: server.URL})
	store.Refresh(context.Background())
	fail = true
	store.Refresh(context.Background())
	if latest, ok := store.Latest("a"); !ok || latest.Tag != "1.0.0" {
		t.Errorf("latest = %+v, %v", latest, ok)
	}
	if time.Since(store.Checked("a")) > time.Minute {
		t.Error("never checked")
	}
}

// TestAppDomain: ${APP_DOMAIN}, the domain alone, needs a domain like
// ${APP_URL} and is no secret.
func TestAppDomain(t *testing.T) {
	entry := Entry{Compose: []byte("services:\n  web:\n    environment:\n      HOST: ${APP_DOMAIN}\n      KEY: ${KEY}\n")}
	if !entry.NeedsURL() {
		t.Error("APP_DOMAIN needs no domain")
	}
	if got := Secrets(entry.Compose); !slices.Equal(got, []string{"KEY"}) {
		t.Errorf("secrets = %v", got)
	}
}

// TestRegistryGHCRPages: GHCR answers 1000 tags a page and links the
// next; releases on a later page count too.
func TestRegistryGHCRPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"token":"t0k"}`))
		case r.URL.Query().Get("last") == "":
			w.Header().Set("Link", `</v2/me/app/tags/list?last=git-1&n=1000>; rel="next"`)
			_, _ = w.Write([]byte(`{"tags":["git-0","git-1"]}`))
		default:
			_, _ = w.Write([]byte(`{"tags":["v0.6.1","v0.6.2"]}`))
		}
	}))
	defer server.Close()
	registry := Registry{Client: server.Client(), HubURL: server.URL, GHCRURL: server.URL}
	tags, err := registry.Tags(context.Background(), "ghcr.io/me/app")
	if err != nil {
		t.Fatal(err)
	}
	if releases := Releases(tags, nil); len(releases) != 2 || releases[0].Tag != "v0.6.2" {
		t.Errorf("releases = %+v", releases)
	}
}
