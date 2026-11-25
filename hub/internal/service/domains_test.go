package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/protocol"
)

// A domain reaches one service of an app: another service of the app
// cannot take it. Another app may, also one of another workspace, so no
// workspace can block a domain for another one.
func TestADomainIsOncePerApp(t *testing.T) {
	apps, servers, _, _ := newAppService(t)
	connectServer(t, servers, other, "web-2")
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(other, "shop", "web-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com", "www.blog.example.com"}, "blog.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(other, "shop", "", []string{"shop.example.com", "Blog.example.com"}, ""); err != nil {
		t.Fatalf("another app sets blog.example.com: err = %v", err)
	}
	var used *repository.DomainUsedError
	if _, err := apps.SetDomains(owner, "blog", "api", []string{"www.blog.example.com"}, ""); !errors.As(err, &used) || used.Name != "www.blog.example.com" {
		t.Fatalf("another service takes www.blog.example.com: err = %v", err)
	}
	// Removing it from one app leaves the other app's.
	if _, err := apps.SetDomains(other, "shop", "", []string{"shop.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	blog, err := apps.Get(owner.WorkspaceID, "blog")
	if domains, _ := blog.DomainsOf(""); err != nil || len(domains) != 2 {
		t.Fatalf("blog domains = %v, %v", domains, err)
	}
}

// The canonical domain moves from one domain to another, a service never
// has two, and the deploy sends the daemon the domains of every service
// as it did when they were JSON on the app.
func TestCanonicalMovesAndReachesTheDaemon(t *testing.T) {
	apps, _, _, daemon := newAppService(t)
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	domains := []string{"blog.example.com", "www.blog.example.com"}
	if _, err := apps.SetDomains(owner, "blog", "", domains, "blog.example.com"); err != nil {
		t.Fatal(err)
	}
	if changed, err := apps.SetDomains(owner, "blog", "", domains, "www.blog.example.com"); err != nil || !changed {
		t.Fatalf("move canonical = %v, %v", changed, err)
	}
	if changed, err := apps.SetDomains(owner, "blog", "", []string{"www.blog.example.com", "blog.example.com"}, "www.blog.example.com"); err != nil || changed {
		t.Fatalf("the same domains in another order = %v, %v", changed, err)
	}
	if _, err := apps.SetDomains(owner, "blog", "api", []string{"api.blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	app, err := apps.Get(owner.WorkspaceID, "blog")
	if err != nil {
		t.Fatal(err)
	}
	mains := 0
	for _, domain := range app.Domains {
		if domain.Main {
			mains++
		}
	}
	if mains != 1 {
		t.Fatalf("main domains = %d, want 1: %+v", mains, app.Domains)
	}

	if _, err := deploySource(apps); err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	got := protocol.Domains{Domains: start.Domains.Domains, Canonical: start.Domains.Canonical, Services: start.Domains.Services}
	want := protocol.Domains{Domains: domains, Canonical: "www.blog.example.com", Services: map[string][]string{"api": {"api.blog.example.com"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deploy.start domains = %+v, want %+v", got, want)
	}
}
