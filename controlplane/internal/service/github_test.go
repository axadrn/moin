package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/protocol"
)

// fakeGitHub answers the GitHub API calls ops makes and records the
// commit statuses.
type fakeGitHub struct {
	mu       sync.Mutex
	statuses []map[string]string
}

func (f *fakeGitHub) serve(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/7/access_tokens":
			if !strings.HasPrefix(auth, "Bearer ey") {
				http.Error(w, "jwt", http.StatusUnauthorized)
				return
			}
			var scope struct {
				Repositories []string          `json:"repositories"`
				Permissions  map[string]string `json:"permissions"`
			}
			json.NewDecoder(r.Body).Decode(&scope)
			token := "ghs_test"
			if len(scope.Repositories) > 0 {
				// A token for one repo that can only read it.
				if len(scope.Repositories) != 1 || scope.Repositories[0] != "blog" || len(scope.Permissions) != 1 || scope.Permissions["contents"] != "read" {
					http.Error(w, "scope", http.StatusUnprocessableEntity)
					return
				}
				token = "ghs_blog_read"
			}
			json.NewEncoder(w).Encode(map[string]any{"token": token, "expires_at": time.Now().Add(time.Hour)})
		case r.URL.Path == "/app/installations/7":
			json.NewEncoder(w).Encode(map[string]any{"account": map[string]string{"login": "axadrn"}})
		case auth != "token ghs_test":
			http.Error(w, "token", http.StatusUnauthorized)
		case r.URL.Path == "/installation/repositories":
			json.NewEncoder(w).Encode(map[string]any{"repositories": []map[string]any{{"full_name": "axadrn/blog", "default_branch": "main", "private": true}}})
		case r.URL.Path == "/repos/axadrn/blog/commits/main" || r.URL.Path == "/repos/axadrn/blog/commits/abc123":
			json.NewEncoder(w).Encode(map[string]any{"sha": "abc123", "commit": map[string]string{"message": "Fix the footer\n\nLonger text"}})
		case strings.HasPrefix(r.URL.Path, "/repos/axadrn/blog/commits/"):
			http.Error(w, "No commit found", http.StatusUnprocessableEntity)
		case r.URL.Path == "/repos/axadrn/blog/tarball/abc123":
			var body bytes.Buffer
			zipped := gzip.NewWriter(&body)
			archive := tar.NewWriter(zipped)
			archive.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader})
			archive.WriteHeader(&tar.Header{Name: "axadrn-blog-abc123/", Typeflag: tar.TypeDir, Mode: 0o755})
			archive.WriteHeader(&tar.Header{Name: "axadrn-blog-abc123/compose.yml", Typeflag: tar.TypeReg, Mode: 0o644, Size: 9})
			archive.Write([]byte("services:"))
			archive.Close()
			zipped.Close()
			w.Write(body.Bytes())
		case r.Method == http.MethodPost && r.URL.Path == "/repos/axadrn/blog/statuses/abc123":
			var status map[string]string
			json.NewDecoder(r.Body).Decode(&status)
			f.mu.Lock()
			f.statuses = append(f.statuses, status)
			f.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
}

func (f *fakeGitHub) waitStatus(t *testing.T, state string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, status := range f.statuses {
			if status["state"] == state {
				f.mu.Unlock()
				return status
			}
		}
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %s commit status, got %v", state, f.statuses)
	return nil
}

func TestGitHubDeploys(t *testing.T) {
	apps, servers, server, agent := newAppService(t)
	fake := &fakeGitHub{}
	githubServer := fake.serve(t)
	defer githubServer.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	github, err := NewGitHub("123", "ops-test", strings.ReplaceAll(string(keyPEM), "\n", `\n`), "hook-secret", githubServer.URL, "https://github.example")
	if err != nil {
		t.Fatal(err)
	}
	_, err = apps.GitHubInstallURL(owner)
	if !errors.Is(err, ErrGitHubOff) {
		t.Fatalf("without the app: err = %v", err)
	}
	apps.SetGitHub(github)
	ctx := context.Background()

	link, err := apps.GitHubInstallURL(owner)
	if err != nil || !strings.HasPrefix(link, "https://github.example/apps/ops-test/installations/new?state=") {
		t.Fatalf("install link = %q, %v", link, err)
	}
	parsed, _ := url.Parse(link)
	state := parsed.Query().Get("state")
	if _, err := apps.ConnectGitHub(ctx, other, 7, state); !errors.Is(err, ErrGitHubState) {
		t.Fatalf("another user's state: err = %v", err)
	}
	account, err := apps.ConnectGitHub(ctx, owner, 7, state)
	if err != nil || account != "axadrn" {
		t.Fatalf("connect = %q, %v", account, err)
	}
	// Installation IDs are public counters: another workspace's own valid
	// state must not move this installation, and its private repos, there.
	otherLink, _ := apps.GitHubInstallURL(other)
	otherParsed, _ := url.Parse(otherLink)
	if _, err := apps.ConnectGitHub(ctx, other, 7, otherParsed.Query().Get("state")); !errors.Is(err, repository.ErrGitHubInstallationTaken) {
		t.Fatalf("another workspace took the installation: err = %v", err)
	}
	if installations, _ := apps.GitHubInstallations(other.WorkspaceID); len(installations) != 0 {
		t.Fatalf("other workspace installations = %+v", installations)
	}
	if installations, _ := apps.GitHubInstallations(owner.WorkspaceID); len(installations) != 1 {
		t.Fatalf("owner lost the installation: %+v", installations)
	}
	if _, err := apps.ConnectGitHub(ctx, owner, 7, state); err != nil {
		t.Fatalf("connect again in the same workspace: %v", err)
	}

	_, err = apps.Create(owner, "blog", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.DeployRef(ctx, owner, "blog", "main"); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("deploy without repo: err = %v", err)
	}
	if _, err := apps.LinkRepo(ctx, owner, "blog", "someone/else", "", true); !errors.Is(err, ErrRepo) {
		t.Fatalf("link a repo ops cannot read: err = %v", err)
	}
	app, err := apps.LinkRepo(ctx, owner, "blog", "https://github.com/axadrn/blog.git", "", true)
	if err != nil || app.Repo != "axadrn/blog" || app.Branch != "main" || !app.AutoDeploy || app.InstallationID != 7 {
		t.Fatalf("link = %+v, %v", app, err)
	}
	if _, err := apps.DeployRef(ctx, owner, "blog", "nope"); !errors.Is(err, ErrRef) {
		t.Fatalf("deploy an unknown ref: err = %v", err)
	}

	deploy, err := apps.DeployRef(ctx, owner, "blog", "")
	if err != nil {
		t.Fatal(err)
	}
	if deploy.Ref != "main" || deploy.CommitSHA != "abc123" || deploy.CommitMessage != "Fix the footer" {
		t.Fatalf("deploy = %+v", deploy)
	}
	var start protocol.DeployStart
	agent.last(t, protocol.TypeDeployStart, &start)
	if start.SourceURL == "" {
		t.Fatal("the agent got no source")
	}
	if !strings.HasSuffix(start.SourceURL, "/repos/axadrn/blog/tarball/abc123") || start.SourceAuthorization != "token ghs_blog_read" || !start.SourceStripTop {
		t.Fatalf("the agent does not download from GitHub itself: %+v", start)
	}
	fake.waitStatus(t, "pending")
	agentSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})
	success := fake.waitStatus(t, "success")
	if success["context"] != "ops/deploy" || !strings.HasSuffix(success["target_url"], "/app/owner-workspace/apps/blog") {
		t.Fatalf("success status = %v", success)
	}

	body := []byte(`{"ref":"refs/heads/main","after":"abc123","installation":{"id":7},"repository":{"full_name":"axadrn/blog"}}`)
	mac := hmac.New(sha256.New, []byte("hook-secret"))
	mac.Write(body)
	if apps.VerifyGitHubWebhook(body, "sha256=00") || !apps.VerifyGitHubWebhook(body, "sha256="+hex.EncodeToString(mac.Sum(nil))) {
		t.Fatal("webhook signature check is wrong")
	}
	apps.GitHubPush(ctx, 7, "axadrn/blog", "other-branch", "abc123")
	apps.GitHubPush(ctx, 7, "axadrn/blog", "main", "abc123")
	_, deploys, err := apps.Deploys(owner.WorkspaceID, "blog", 10)
	if err != nil || len(deploys) != 2 || deploys[0].CommitSHA != "abc123" {
		t.Fatalf("after the push: %d deploys, %v", len(deploys), err)
	}

	err = apps.GitHubUninstalled(7)
	if err != nil {
		t.Fatal(err)
	}
	app, _ = apps.Get(owner.WorkspaceID, "blog")
	if app.Repo != "" || app.InstallationID != 0 {
		t.Fatalf("uninstall left the app linked: %+v", app)
	}
}
