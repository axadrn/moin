package service

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// daemonReports sends the domains of compose.yml as a daemon does before a
// release starts, and returns the hub's answer.
func daemonReports(t *testing.T, servers *ServerService, serverID string, daemon *fakeDaemon, domains protocol.DeployDomains) protocol.Message {
	t.Helper()
	message, err := protocol.NewMessage(protocol.TypeDeployDomains, domains)
	if err != nil {
		t.Fatal(err)
	}
	message.RequestID = rand.Text()
	servers.Receive(serverID, message)
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	for _, sent := range daemon.sent {
		if sent.RequestID == message.RequestID {
			return sent
		}
	}
	t.Fatal("the hub did not answer")
	return protocol.Message{}
}

// hubError is the message of an error answer, "" for ok.
func hubError(answer protocol.Message) string {
	if answer.Type != protocol.TypeError {
		return ""
	}
	var refused protocol.Error
	_ = answer.Decode(&refused)
	return refused.Message
}

// domainRows are the domains of an app as rows say them: service, name, main
// and from compose.yml.
func domainRows(app *model.App) []string {
	list := []string{}
	for _, domain := range app.Domains {
		row := domain.Service + ":" + domain.Name
		if domain.Main {
			row += " main"
		}
		if domain.FromCompose {
			row += " compose"
		}
		list = append(list, row)
	}
	slices.Sort(list)
	return list
}

// If compose.yml has domains, exactly those count. Otherwise the ones
// entered in moin. A deploy replaces the entered ones with the ones of
// compose.yml, a line taken out of the file goes, and the whole block
// taken out leaves none; while they come from compose.yml, moin refuses
// to change them on every surface.
func TestComposeDomainsFollowEachDeploy(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"old.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "admin", []string{"admin.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	deployWith := func(domains protocol.DeployDomains) {
		t.Helper()
		deploy, err := deploySource(apps)
		if err != nil {
			t.Fatal(err)
		}
		domains.DeployID = deploy.ID
		if answer := daemonReports(t, servers, server.ID, daemon, domains); answer.Type != protocol.TypeOK {
			t.Fatalf("report %+v: %s", domains, hubError(answer))
		}
		daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true, Detected: protocol.Detected{Kind: "compose", Service: "web"}})
	}
	rows := func() []string {
		t.Helper()
		app, err := apps.Get(owner.WorkspaceID, "blog")
		if err != nil {
			t.Fatal(err)
		}
		return domainRows(app)
	}

	// The first deploy with x-moin.domains replaces the ones entered in moin.
	deployWith(protocol.DeployDomains{Domains: []string{"Shop.example.com", "www.shop.example.com"}, Canonical: "shop.example.com", Services: map[string][]string{"api": {"api.shop.example.com"}}})
	want := []string{":shop.example.com main compose", ":www.shop.example.com compose", "api:api.shop.example.com compose"}
	if got := rows(); !slices.Equal(got, want) {
		t.Fatalf("after the first deploy = %v, want %v", got, want)
	}
	// moin refuses changes while compose.yml decides.
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); !errors.Is(err, ErrComposeDomains) {
		t.Fatalf("set main domains: err = %v", err)
	}
	if _, err := apps.SetDomains(owner, "blog", "worker", []string{"worker.example.com"}, ""); !errors.Is(err, ErrComposeDomains) {
		t.Fatalf("set service domains: err = %v", err)
	}
	checks, err := apps.Domains(owner.WorkspaceID, "blog")
	if err != nil || len(checks) != 3 || !checks[0].FromCompose || !checks[0].Canonical || !checks[2].FromCompose || checks[2].Service != "api" {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	// The next deploy gets none of them: the daemon reads them from the
	// file of its release.
	next, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if len(start.Domains.Domains) != 0 || start.Domains.Canonical != "" || start.Domains.Services != nil {
		t.Fatalf("deploy.start sends the domains of compose.yml: %+v", start.Domains)
	}

	// A line taken out of compose.yml goes.
	if answer := daemonReports(t, servers, server.ID, daemon, protocol.DeployDomains{DeployID: next.ID, Domains: []string{"shop.example.com"}, Canonical: "shop.example.com"}); answer.Type != protocol.TypeOK {
		t.Fatal(hubError(answer))
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: next.ID, OK: true})
	if got := rows(); !slices.Equal(got, []string{":shop.example.com main compose"}) {
		t.Fatalf("after a line went = %v", got)
	}

	// The whole block taken out leaves none, and moin decides again.
	deployWith(protocol.DeployDomains{})
	if got := rows(); len(got) != 0 {
		t.Fatalf("after the block went = %v", got)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	// A deploy without x-moin.domains keeps the ones entered in moin.
	deployWith(protocol.DeployDomains{})
	if got := rows(); !slices.Equal(got, []string{":blog.example.com"}) {
		t.Fatalf("entered domains after a deploy without x-moin = %v", got)
	}
}

// The hub checks the domains of compose.yml as it checks the ones entered
// in moin: a name another app uses, one with a port or a canonical one
// outside the list is refused, which stops the deploy, and the app keeps
// the domains it had. Only the daemon of the app's server reports, and
// only for a deploy that runs.
func TestComposeDomainsAreChecked(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	otherServer, otherDaemon := connectServer(t, servers, other, "web-2")
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(other, "shop", "web-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(other, "shop", "", []string{"shop.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	deploy, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		domains protocol.DeployDomains
		refusal string
	}{
		{protocol.DeployDomains{Domains: []string{"blog.example.com", "shop.example.com"}}, "shop.example.com is already used by another app"},
		{protocol.DeployDomains{Domains: []string{"blog.example.com:8080"}}, "domains must be host names"},
		{protocol.DeployDomains{Services: map[string][]string{"api": {"localhost:3000"}}}, "domains must be host names"},
		{protocol.DeployDomains{Domains: []string{"blog.example.com"}, Canonical: "www.blog.example.com"}, "canonical domain must be one of the domains"},
		{protocol.DeployDomains{Services: map[string][]string{"API!": {"api.blog.example.com"}}}, "a service name is"},
	} {
		test.domains.DeployID = deploy.ID
		if refusal := hubError(daemonReports(t, servers, server.ID, daemon, test.domains)); !strings.Contains(refusal, test.refusal) {
			t.Errorf("%+v: refusal %q, want %q", test.domains, refusal, test.refusal)
		}
	}
	// Another server's daemon cannot set the domains of this deploy.
	if refusal := hubError(daemonReports(t, servers, otherServer.ID, otherDaemon, protocol.DeployDomains{DeployID: deploy.ID, Domains: []string{"taken.example.com"}})); refusal != ErrNotDeploying.Error() {
		t.Errorf("another server: refusal %q", refusal)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, Error: "domains: shop.example.com is already used by another app or service"})
	// Nor can a deploy that ended.
	if refusal := hubError(daemonReports(t, servers, server.ID, daemon, protocol.DeployDomains{DeployID: deploy.ID, Domains: []string{"late.example.com"}})); refusal != ErrNotDeploying.Error() {
		t.Errorf("an ended deploy: refusal %q", refusal)
	}
	blog, err := apps.Get(owner.WorkspaceID, "blog")
	if err != nil || !slices.Equal(domainRows(blog), []string{":blog.example.com"}) {
		t.Fatalf("blog after refusals = %v, %v", domainRows(blog), err)
	}
}

// Every reader takes the domains from the table, never from what the live
// deploy found: the API, the checks, uptime and the tunnel.
func TestDomainsComeOnlyFromTheTable(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	deploy, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	// A deploy that found other domains, but never reported them.
	detected := protocol.Detected{Kind: "compose", Service: "web", Domains: []string{"found.example.com"}, ServiceDomains: map[string][]string{"api": {"api.found.example.com"}}, Config: &protocol.Config{Domains: []string{"found.example.com"}}}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true, Detected: detected})
	app, err := apps.Get(owner.WorkspaceID, "blog")
	if err != nil {
		t.Fatal(err)
	}
	if domains, _ := app.DomainsOf(""); !slices.Equal(domains, []string{"blog.example.com"}) || len(app.ServiceDomains()) != 0 || app.DomainsFromCompose() {
		t.Fatalf("app domains = %v, %v", domains, app.ServiceDomains())
	}
	if got := uptimeDomains(app); !slices.Equal(got, []string{"blog.example.com"}) {
		t.Fatalf("uptime visits %v", got)
	}
	checks, err := apps.Domains(owner.WorkspaceID, "blog")
	if err != nil || len(checks) != 1 || checks[0].Domain != "blog.example.com" || checks[0].FromCompose {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	hosts, err := apps.tunnelHosts(server)
	if err != nil || !slices.Equal(hosts, []string{"blog.example.com"}) {
		t.Fatalf("tunnel hosts = %v, %v", hosts, err)
	}
	if _, err := apps.CheckDomains(context.Background(), owner.WorkspaceID, "blog"); err != nil {
		t.Fatal(err)
	}
}
