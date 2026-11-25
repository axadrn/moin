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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/protocol"
)

// fakeGitHub answers the GitHub API calls moin makes and records the
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
		case r.Method == http.MethodPost && r.URL.Path == "/login/oauth/access_token":
			// The code GitHub sends after the install, traded with the
			// app's client ID and secret for a token of the user.
			var grant map[string]string
			json.NewDecoder(r.Body).Decode(&grant)
			tokens := map[string]string{"code-axadrn": "ghu_axadrn", "code-mallory": "ghu_mallory", "code-nobody": "ghu_nobody"}
			if grant["client_id"] != "Iv1.test" || grant["client_secret"] != "client-secret" || tokens[grant["code"]] == "" || !strings.HasPrefix(grant["redirect_uri"], "https://") {
				json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"access_token": tokens[grant["code"]], "token_type": "bearer"})
		case r.URL.Path == "/user/installations":
			// axadrn reaches installation 7, mallory only her own 9, nobody
			// none.
			installations := map[string][]map[string]any{"Bearer ghu_axadrn": {{"id": 7}}, "Bearer ghu_mallory": {{"id": 9}}, "Bearer ghu_nobody": {}}
			found, ok := installations[auth]
			if !ok {
				http.Error(w, "token", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"total_count": len(found), "installations": found})
		case r.URL.Path == "/user":
			users := map[string]map[string]any{"Bearer ghu_axadrn": {"id": 1, "login": "axadrn", "name": "Ax"}, "Bearer ghu_mallory": {"id": 2, "login": "mallory", "name": nil}}
			if _, ok := users[auth]; !ok {
				http.Error(w, "token", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(users[auth])
		case r.URL.Path == "/user/emails":
			// axadrn has a verified primary and a secondary address;
			// mallory added someone else's as primary, unverified.
			emails := map[string][]map[string]any{
				"Bearer ghu_axadrn":  {{"email": "work@example.com", "primary": false, "verified": true}, {"email": "ada@example.com", "primary": true, "verified": true}},
				"Bearer ghu_mallory": {{"email": "victim@example.com", "primary": true, "verified": false}, {"email": "mallory@example.com", "primary": false, "verified": true}},
			}
			json.NewEncoder(w).Encode(emails[auth])
		case auth != "token ghs_test":
			http.Error(w, "token", http.StatusUnauthorized)
		case r.URL.Path == "/installation/repositories":
			json.NewEncoder(w).Encode(map[string]any{"repositories": []map[string]any{{"full_name": "axadrn/blog", "default_branch": "main", "private": true}}})
		case r.URL.Path == "/repos/axadrn/blog/branches":
			json.NewEncoder(w).Encode([]map[string]string{{"name": "main"}, {"name": "dev"}})
		case r.URL.Path == "/repos/axadrn/blog/commits/main" || r.URL.Path == "/repos/axadrn/blog/commits/abc123":
			json.NewEncoder(w).Encode(map[string]any{"sha": "abc123", "commit": map[string]string{"message": "Fix the footer\n\nLonger text"}})
		// c0ffee1 is on main; f0f0f0f is a fork's commit GitHub serves
		// through the repo, and not on main.
		case r.URL.Path == "/repos/axadrn/blog/commits/c0ffee1" || r.URL.Path == "/repos/axadrn/blog/commits/f0f0f0f":
			sha := strings.TrimPrefix(r.URL.Path, "/repos/axadrn/blog/commits/")
			json.NewEncoder(w).Encode(map[string]any{"sha": sha, "commit": map[string]string{"message": "A commit"}})
		case r.URL.Path == "/repos/axadrn/blog/compare/main...c0ffee1":
			json.NewEncoder(w).Encode(map[string]string{"status": "behind"})
		case r.URL.Path == "/repos/axadrn/blog/compare/main...f0f0f0f":
			json.NewEncoder(w).Encode(map[string]string{"status": "diverged"})
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
	apps, servers, server, daemon := newAppService(t)
	fake := &fakeGitHub{}
	githubServer := fake.serve(t)
	defer githubServer.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	github, err := NewGitHub("123", "moin-test", strings.ReplaceAll(string(keyPEM), "\n", `\n`), "hook-secret", "", "", githubServer.URL, "https://github.example")
	if err != nil {
		t.Fatal(err)
	}
	_, err = apps.GitHubInstallURL(owner)
	if !errors.Is(err, ErrGitHubOff) {
		t.Fatalf("without the app: err = %v", err)
	}
	apps.UseGitHubApp(githubAppOf(github))
	ctx := context.Background()

	link, err := apps.GitHubInstallURL(owner)
	if err != nil || !strings.HasPrefix(link, "https://github.example/apps/moin-test/installations/new?state=") {
		t.Fatalf("install link = %q, %v", link, err)
	}
	parsed, _ := url.Parse(link)
	state := parsed.Query().Get("state")
	if _, err := apps.ConnectGitHub(ctx, other, 7, state, ""); !errors.Is(err, ErrGitHubState) {
		t.Fatalf("another user's state: err = %v", err)
	}
	if link, err := apps.ConnectGitHub(ctx, owner, 7, state, ""); err != nil || link != "" {
		t.Fatalf("connect = %q, %v", link, err)
	}
	if installations, _ := apps.GitHubInstallations(owner.WorkspaceID); len(installations) != 1 || installations[0].Account != "axadrn" {
		t.Fatalf("owner installations = %+v", installations)
	}
	if link, _ := apps.GitHubConnectURL(owner); !strings.HasPrefix(link, "https://github.example/apps/moin-test/installations/new?state=") {
		t.Fatalf("without OAuth the connect link installs: %q", link)
	}
	// Installation IDs are public counters: another workspace's own valid
	// state must not move this installation, and its private repos, there.
	otherLink, _ := apps.GitHubInstallURL(other)
	otherParsed, _ := url.Parse(otherLink)
	if _, err := apps.ConnectGitHub(ctx, other, 7, otherParsed.Query().Get("state"), ""); !errors.Is(err, repository.ErrGitHubInstallationTaken) {
		t.Fatalf("another workspace took the installation: err = %v", err)
	}
	if installations, _ := apps.GitHubInstallations(other.WorkspaceID); len(installations) != 0 {
		t.Fatalf("other workspace installations = %+v", installations)
	}
	if installations, _ := apps.GitHubInstallations(owner.WorkspaceID); len(installations) != 1 {
		t.Fatalf("owner lost the installation: %+v", installations)
	}
	if _, err := apps.ConnectGitHub(ctx, owner, 7, state, ""); err != nil {
		t.Fatalf("connect again in the same workspace: %v", err)
	}

	_, err = apps.Create(owner, "blog", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.DeployRef(ctx, owner, "blog", "main"); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("deploy without repo: err = %v", err)
	}
	// Unlinking an app that follows no repo changes nothing, the activity included.
	if err := apps.UnlinkRepo(owner, "blog"); err != nil {
		t.Fatalf("unlink without a repo: %v", err)
	}
	if events, _ := servers.activity.ListPage(owner.WorkspaceID, model.ActivityFilter{}, "", 50); slices.ContainsFunc(events, func(e model.ActivityEvent) bool { return e.Action == "repo.unlinked" }) {
		t.Fatal("unlink without a repo shows in the activity")
	}
	if _, err := apps.LinkRepo(ctx, owner, "blog", "someone/else", "", true); !errors.Is(err, ErrRepo) {
		t.Fatalf("link a repo moin cannot read: err = %v", err)
	}
	if _, err := apps.LinkRepo(ctx, owner, "blog", "axadrn/blog", "nope", true); !errors.Is(err, ErrBranch) {
		t.Fatalf("link a branch GitHub does not have: err = %v", err)
	}
	if app, err := apps.LinkRepo(ctx, owner, "blog", "axadrn/blog", "dev", false); err != nil || app.Branch != "dev" {
		t.Fatalf("link dev = %+v, %v", app, err)
	}
	app, err := apps.LinkRepo(ctx, owner, "blog", "https://github.com/axadrn/blog.git", "", true)
	if err != nil || app.Repo != "axadrn/blog" || app.Branch != "main" || !app.AutoDeploy || app.InstallationID != 7 {
		t.Fatalf("link = %+v, %v", app, err)
	}
	// One thing brings new versions: a deploy hook turns deploy on push
	// off and keeps the repo for the compose file; turning deploy on push
	// on turns the hook off.
	if _, err := apps.RotateDeployHook(owner, "blog"); err != nil {
		t.Fatal(err)
	}
	if hooked, _ := apps.Get(owner.WorkspaceID, "blog"); hooked.Repo != "axadrn/blog" || hooked.AutoDeploy || hooked.DeployHookHash == nil || hooked.DeployFrom() != "ci" {
		t.Fatalf("a deploy hook: %+v", hooked)
	}
	app, err = apps.LinkRepo(ctx, owner, "blog", "axadrn/blog", "", true)
	if err != nil || app.Repo != "axadrn/blog" || !app.AutoDeploy || app.DeployHookHash != nil || app.DeployFrom() != "github" {
		t.Fatalf("deploy on push kept the deploy hook: %+v, %v", app, err)
	}
	if _, err := apps.DeployRef(ctx, owner, "blog", "nope"); !errors.Is(err, ErrRef) {
		t.Fatalf("deploy an unknown ref: err = %v", err)
	}
	if _, err := apps.DeployRef(ctx, owner, "blog", "f0f0f0f"); !errors.Is(err, ErrNotOnBranch) {
		t.Fatalf("deploy a fork's commit: err = %v", err)
	}
	if deploy, err := apps.DeployRef(ctx, owner, "blog", "c0ffee1"); err != nil || deploy.CommitSHA != "c0ffee1" {
		t.Fatalf("deploy a commit on the branch: %+v, %v", deploy, err)
	}
	finishDeploys(t, apps, app.ID)

	deploy, err := apps.DeployRef(ctx, owner, "blog", "")
	if err != nil {
		t.Fatal(err)
	}
	if deploy.Ref != "main" || deploy.CommitSHA != "abc123" || deploy.CommitMessage != "Fix the footer" {
		t.Fatalf("deploy = %+v", deploy)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.SourceURL == "" {
		t.Fatal("the daemon got no source")
	}
	if !strings.HasSuffix(start.SourceURL, "/repos/axadrn/blog/tarball/abc123") || start.SourceAuthorization != "token ghs_blog_read" || !start.SourceStripTop {
		t.Fatalf("the daemon does not download from GitHub itself: %+v", start)
	}
	fake.waitStatus(t, "pending")
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})
	success := fake.waitStatus(t, "success")
	if success["context"] != "moin/deploy" || !strings.HasSuffix(success["target_url"], "/apps/"+app.Link()) {
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
	if err != nil || len(deploys) != 3 || deploys[0].CommitSHA != "abc123" {
		t.Fatalf("after the push: %d deploys, %v", len(deploys), err)
	}
	// A push deploy names the branch, not the commit twice.
	if deploys[0].Ref != "main" {
		t.Fatalf("push deploy ref = %q, want main", deploys[0].Ref)
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

// TestGitHubChecksInstaller: with the app's client ID and secret, moin
// connects an installation only for a member whose GitHub account may
// manage it.
func TestGitHubChecksInstaller(t *testing.T) {
	apps, _, _, _ := newAppService(t)
	githubServer := (&fakeGitHub{}).serve(t)
	defer githubServer.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	github, err := NewGitHub("123", "moin-test", string(keyPEM), "hook-secret", "Iv1.test", "client-secret", githubServer.URL, githubServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	apps.UseGitHubApp(githubAppOf(github))
	ctx := context.Background()
	stateOf := func(actor Actor) string {
		link, _ := apps.GitHubInstallURL(actor)
		parsed, _ := url.Parse(link)
		return parsed.Query().Get("state")
	}

	// A wrong code, or a GitHub user who cannot reach the installation:
	// refused.
	for _, code := range []string{"code-wrong", "code-mallory"} {
		if _, err := apps.ConnectGitHub(ctx, other, 7, stateOf(other), code); !errors.Is(err, ErrGitHubNotYours) {
			t.Fatalf("code %q: err = %v", code, err)
		}
	}
	if installations, _ := apps.GitHubInstallations(other.WorkspaceID); len(installations) != 0 {
		t.Fatalf("other workspace took installation 7: %+v", installations)
	}
	link, err := apps.ConnectGitHub(ctx, owner, 7, stateOf(owner), "code-axadrn")
	if err != nil || link != "" {
		t.Fatalf("connect = %q, %v", link, err)
	}
	if installations, _ := apps.GitHubInstallations(owner.WorkspaceID); len(installations) != 1 || installations[0].Account != "axadrn" {
		t.Fatalf("owner installations = %+v", installations)
	}
}

// TestGitHubAlreadyInstalled: for an app that is already installed GitHub
// shows its settings and sends no install back, so "Connect GitHub" signs
// in to GitHub first and connects the installations the member has.
func TestGitHubAlreadyInstalled(t *testing.T) {
	apps, _, _, _ := newAppService(t)
	githubServer := (&fakeGitHub{}).serve(t)
	defer githubServer.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	github, err := NewGitHub("123", "moin-test", string(keyPEM), "hook-secret", "Iv1.test", "client-secret", githubServer.URL, githubServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	apps.UseGitHubApp(githubAppOf(github))
	ctx := context.Background()

	link, err := apps.GitHubConnectURL(owner)
	if err != nil || !strings.HasPrefix(link, githubServer.URL+"/login/oauth/authorize?client_id=Iv1.test&redirect_uri=") {
		t.Fatalf("connect link = %q, %v", link, err)
	}
	parsed, _ := url.Parse(link)
	state := parsed.Query().Get("state")
	if redirect := parsed.Query().Get("redirect_uri"); !strings.HasSuffix(redirect, "/app/github/setup") {
		t.Fatalf("redirect_uri = %q, want the setup URL", redirect)
	}

	// After a change of the repos ("Redirect on update") GitHub sends the
	// installation without a code: moin asks for a sign-in with the same
	// state, which comes back with a code.
	if next, err := apps.ConnectGitHub(ctx, owner, 7, state, ""); err != nil || next != link {
		t.Fatalf("update without a code: %q, %v", next, err)
	}
	// Someone who installed the app nowhere installs it now.
	if next, err := apps.ConnectGitHub(ctx, owner, 0, state, "code-nobody"); err != nil || next != githubServer.URL+"/apps/moin-test/installations/new?state="+url.QueryEscape(state) {
		t.Fatalf("no installation yet: %q, %v", next, err)
	}
	// The sign-in comes back with a code alone: moin connects installation 7.
	if next, err := apps.ConnectGitHub(ctx, owner, 0, state, "code-axadrn"); err != nil || next != "" {
		t.Fatalf("sign-in: %q, %v", next, err)
	}
	if installations, _ := apps.GitHubInstallations(owner.WorkspaceID); len(installations) != 1 || installations[0].InstallationID != 7 {
		t.Fatalf("owner installations = %+v", installations)
	}
	// Another workspace of the same GitHub user cannot take it.
	otherLink, _ := apps.GitHubConnectURL(other)
	otherParsed, _ := url.Parse(otherLink)
	if _, err := apps.ConnectGitHub(ctx, other, 0, otherParsed.Query().Get("state"), "code-axadrn"); !errors.Is(err, repository.ErrGitHubInstallationTaken) {
		t.Fatalf("another workspace: err = %v", err)
	}
	if installations, _ := apps.GitHubInstallations(other.WorkspaceID); len(installations) != 0 {
		t.Fatalf("other workspace installations = %+v", installations)
	}
}

// TestImageDeploysTakeTheCommit: an image deploy of an app that follows a
// repo takes the compose file of a commit there, the one CI sends to the
// hook or names by hand, else the head of the branch. Turning the hook
// off leaves the link as it is.
func TestImageDeploysTakeTheCommit(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	githubServer := (&fakeGitHub{}).serve(t)
	defer githubServer.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	github, err := NewGitHub("123", "moin-test", string(keyPEM), "hook-secret", "", "", githubServer.URL, githubServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	apps.UseGitHubApp(githubAppOf(github))
	ctx := context.Background()
	link, _ := apps.GitHubInstallURL(owner)
	parsed, _ := url.Parse(link)
	if _, err := apps.ConnectGitHub(ctx, owner, 7, parsed.Query().Get("state"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(owner, "blog", server.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.LinkRepo(ctx, owner, "blog", "axadrn/blog", "", true); err != nil {
		t.Fatal(err)
	}
	hook, err := apps.RotateDeployHook(owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
	token := hook[strings.LastIndex(hook, "/")+1:]
	deployed := func(deploy *model.Deploy, image, ref string) {
		t.Helper()
		var start protocol.DeployStart
		daemon.last(t, protocol.TypeDeployStart, &start)
		if start.Image != image || !strings.HasSuffix(start.SourceURL, "/repos/axadrn/blog/tarball/abc123") || !start.SourceStripTop {
			t.Fatalf("deploy.start = %+v", start)
		}
		if deploy.Image != image || deploy.Ref != ref || deploy.CommitSHA != "abc123" || deploy.CommitMessage != "Fix the footer" {
			t.Fatalf("deploy = %+v", deploy)
		}
		daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true})
	}

	// By hand: the head of the branch, or the ref named.
	first, err := apps.Deploy(ctx, owner, "blog", "ghcr.io/me/blog:v1", "")
	if err != nil {
		t.Fatal(err)
	}
	deployed(first, "ghcr.io/me/blog:v1", "main")
	second, err := apps.Deploy(ctx, owner, "blog", "ghcr.io/me/blog:v2", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	deployed(second, "ghcr.io/me/blog:v2", "abc123")

	// The hook: the commit CI sends, else the head of the branch.
	_, deploy, err := apps.DeployHook(ctx, token, "ghcr.io/me/blog:abc", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	deployed(deploy, "ghcr.io/me/blog:abc", "abc123")
	if _, deploy, err = apps.DeployHook(ctx, token, "ghcr.io/me/blog:def", ""); err != nil {
		t.Fatal(err)
	}
	deployed(deploy, "ghcr.io/me/blog:def", "main")
	if _, _, err := apps.DeployHook(ctx, token, "ghcr.io/me/blog:ghi", "--upload-pack=x"); !errors.Is(err, ErrRefName) {
		t.Fatalf("a commit that is no name: err = %v", err)
	}
	if _, _, err := apps.DeployHook(ctx, token, "ghcr.io/me/blog:ghi", "def456"); !errors.Is(err, ErrRef) {
		t.Fatalf("a commit GitHub does not know: err = %v", err)
	}
	var refused *HookImageError
	if _, _, err := apps.DeployHook(ctx, token, "nginx:1.27", "abc123"); !errors.As(err, &refused) {
		t.Fatalf("hook deployed another image: err = %v", err)
	}

	// Turning the hook off leaves the link; pushes still do not deploy.
	if _, err := apps.RemoveDeployHook(owner, "blog"); err != nil {
		t.Fatal(err)
	}
	if app, _ := apps.Get(owner.WorkspaceID, "blog"); app.Repo != "axadrn/blog" || app.AutoDeploy || app.DeployFrom() != "github" {
		t.Fatalf("after the hook: %+v", app)
	}
	// Deploy on push turns a hook off.
	if _, err := apps.RotateDeployHook(owner, "blog"); err != nil {
		t.Fatal(err)
	}
	if app, err := apps.SetAutoDeploy(owner, "blog", true); err != nil || !app.AutoDeploy || app.DeployHookHash != nil {
		t.Fatalf("deploy on push = %+v, %v", app, err)
	}
}

// TestGitHubAccount: a sign-in with the app gives the GitHub account with
// its verified primary email only. A secondary address or an unverified
// primary one never counts.
func TestGitHubAccount(t *testing.T) {
	githubServer := (&fakeGitHub{}).serve(t)
	defer githubServer.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	github, err := NewGitHub("123", "moin-test", string(keyPEM), "hook-secret", "Iv1.test", "client-secret", githubServer.URL, githubServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const callback = "https://moin.example.com/auth/github/callback"

	link, _ := url.Parse(github.AuthorizeURL("state-1", callback))
	if link.Query().Get("redirect_uri") != callback || link.Query().Get("state") != "state-1" || link.Query().Get("client_id") != "Iv1.test" {
		t.Fatalf("authorize URL = %s", link)
	}
	account, err := github.Account(ctx, "code-axadrn", callback)
	if err != nil || account != (GitHubAccount{ID: 1, Login: "axadrn", Name: "Ax", Email: "ada@example.com"}) {
		t.Fatalf("Account() = %+v, %v", account, err)
	}
	account, err = github.Account(ctx, "code-mallory", callback)
	if err != nil || account.ID != 2 || account.Email != "" {
		t.Fatalf("an unverified primary email: Account() = %+v, %v", account, err)
	}
	if _, err := github.Account(ctx, "code-wrong", callback); err == nil {
		t.Fatal("a wrong code signed in")
	}
	if _, err := github.Account(ctx, "code-axadrn", ""); err == nil {
		t.Fatal("a code without the redirect URI signed in")
	}
}
