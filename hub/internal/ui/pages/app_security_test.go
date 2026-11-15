package pages

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/components"
	"github.com/axadrn/moin/protocol"
)

// Each line says what is on the server: green when locked down as set,
// quiet when open as set, red with both words when it differs, waiting
// before the daemon reported.
func TestSecurityLinesTellTheTruth(t *testing.T) {
	server := model.Server{Tunnel: true, SSH: protocol.SSHOff, Firewall: true}
	report := &protocol.ServerConfigResult{Proxy: "direct", SSH: protocol.SSHEveryone, Firewall: true, OpenPorts: []string{"22/tcp"}}
	for _, test := range []struct {
		line  securityLine
		state components.State
		note  string
	}{
		{visitorsLine(server, report), components.StateFailed, "Ports 80 and 443 open · set: Cloudflare Tunnel"},
		{sshLine(server, report), components.StateFailed, "Everyone · set: Off"},
		{firewallLine(server, report), components.StateLive, "Open to everyone: 22/tcp"},
		{sshLine(model.Server{SSH: protocol.SSHTailnet}, &protocol.ServerConfigResult{SSH: protocol.SSHTailnet, TailscaleIP: "100.64.0.9"}), components.StateLive, "Only in the tailnet · 100.64.0.9"},
		{visitorsLine(model.Server{}, &protocol.ServerConfigResult{Proxy: "direct"}), components.StateIdle, "Ports 80 and 443 open"},
		{firewallLine(model.Server{Firewall: true}, nil), components.StateWaiting, "On, once the daemon reports"},
	} {
		if test.line.State != test.state || test.line.Note != test.note {
			t.Errorf("%s: %s %q, want %s %q", test.line.Name, test.line.State, test.line.Note, test.state, test.note)
		}
	}
}

// Without Cloudflare and a tailnet the lines open Integrations; the page
// holds no key field.
func TestSecurityPageSendsKeysToIntegrations(t *testing.T) {
	var html strings.Builder
	view := SecurityView{Server: model.Server{ID: "0123456789abcdef0123456789abcdef", Name: "web-1", SSH: protocol.SSHEveryone, Security: `{"proxy":"direct","ssh":"everyone","firewall":false,"open_ports":[]}`}}
	if err := ServerSecurity(view).Render(projectPageContext("/app/tiny-rocket-co/servers/web-1/security"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{"Connect Cloudflare in Settings, Integrations first", "Connect Tailscale in Settings, Integrations first", `href="/app/tiny-rocket-co/integrations"`, `data-security-line="firewall"`, "Visitors", "Firewall"} {
		if !strings.Contains(page, want) {
			t.Errorf("security page misses %q", want)
		}
	}
	for _, unwanted := range []string{"cloudflare_token", "tailscale_key", "Apply"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("security page still has %q", unwanted)
		}
	}
}

// Integrations: connected accounts with a green dot and Disconnect at the
// end; the others with their key field and Connect.
func TestIntegrationsPage(t *testing.T) {
	var html strings.Builder
	view := IntegrationsView{GitHub: true, GitHubAccounts: []model.GitHubInstallation{{Account: "acme"}}, Keys: service.Integrations{Cloudflare: true, CloudflareAccount: "acc1"}}
	if err := Integrations(view).Render(projectPageContext("/app/tiny-rocket-co/integrations"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{">Integrations</h1>", "acme", "Connect another account", "Account acc1", "Disconnect Cloudflare", `name="tailscale_key"`, "Connect"} {
		if !strings.Contains(page, want) {
			t.Errorf("integrations page misses %q", want)
		}
	}
	if strings.Contains(page, `name="cloudflare_token"`) || strings.Contains(page, "Disconnect Tailscale") {
		t.Error("a connected account asks for its key, or a missing one can be disconnected")
	}
}
