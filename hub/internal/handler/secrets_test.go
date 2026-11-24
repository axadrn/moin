package handler

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// TestSecretsOnServersWeb: a new app shows its instant password once in
// place of the form, New password shows another once, and the keys of
// buckets and registries go to a server: the page says which keep them
// and refuses an offline one in its own words.
func TestSecretsOnServersWeb(t *testing.T) {
	s := newSwitches(t)
	server, err := s.servers.Get(s.ada.Workspace.ID, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	s.servers.Connect(server, testutil.Hello, "203.0.113.5", func() {}, testutil.AnswerOK(s.servers.Receive, server.ID))
	base := "/app/" + s.ada.Workspace.Slug

	added := s.post("/app/{workspaceSlug}/apps", base+"/apps", url.Values{"name": {"shop"}, "server": {"web-1"}}, s.handler.Create)
	if !strings.Contains(added, "data-app-added") || !strings.Contains(added, "data-instant-password") || !strings.Contains(added, "only this once") {
		t.Fatalf("create: %s", added)
	}

	before := s.app().InstantPasswordHash
	fresh := s.post("/app/{workspaceSlug}/apps/{app}/domains/instant-new-password", base+"/apps/blog/domains/instant-new-password", url.Values{}, s.handler.NewInstantPassword)
	if !strings.Contains(fresh, "data-instant-password") || !strings.Contains(fresh, "Shown once.") || s.app().InstantPasswordHash == before {
		t.Fatalf("new password: %s", fresh)
	}
	if again := s.post("/app/{workspaceSlug}/apps/{app}/domains/detail", base+"/apps/blog/domains/detail", url.Values{}, s.handler.DomainsDetail); strings.Contains(again, "data-instant-password") {
		t.Fatalf("the password shows twice: %s", again)
	}

	registries := s.post("/app/{workspaceSlug}/registries", base+"/registries", url.Values{"host": {"ghcr.io"}, "username": {"ada"}, "password": {"registry-pass-123"}}, s.handler.SetRegistry)
	if !strings.Contains(registries, "On web-1.") || strings.Contains(registries, "registry-pass-123") {
		t.Fatalf("registry: %s", registries)
	}
	if _, err := s.apps.AddBucket(context.Background(), s.actor(), service.BucketInput{Name: "main", Endpoint: "https://s3.example.com", Bucket: "main", AccessKey: "k", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.servers.Create(s.actor(), "web-2"); err != nil {
		t.Fatal(err)
	}
	refused := s.post("/app/{workspaceSlug}/bucket/{name}/key", base+"/bucket/main/key", url.Values{"server": {"web-2"}, "secret_key": {"s"}}, s.handler.SetBucketKey)
	if !strings.Contains(refused, "web-2 is offline. Try again when it is back.") {
		t.Fatalf("a key for an offline server: %s", refused)
	}
}
