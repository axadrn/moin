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
// quiet when open as set, red with a sentence that says both when it
// differs, waiting before the daemon reported.
func TestSecurityLinesTellTheTruth(t *testing.T) {
	server := model.Server{Tunnel: true, SSH: protocol.SSHOff, Firewall: true}
	report := &protocol.ServerConfigResult{Proxy: "direct", SSH: protocol.SSHEveryone, Firewall: true, OpenPorts: []string{"22/tcp"}}
	for _, test := range []struct {
		line    securityLine
		state   components.State
		note    string
		problem string
	}{
		{webLine(server, report), components.StateFailed, "", "Web on the server is Open. moin says Cloudflare Tunnel. Pick the one you want."},
		{sshLine(server, report), components.StateFailed, "", "SSH on the server is Everyone. moin says Off. Pick the one you want."},
		{firewallLine(server, report), components.StateLive, "Open to everyone: 22", ""},
		{sshLine(model.Server{SSH: protocol.SSHTailscale}, &protocol.ServerConfigResult{SSH: protocol.SSHTailscale, TailscaleIP: "100.64.0.9"}), components.StateLive, "Tailnet address: 100.64.0.9", ""},
		{sshLine(model.Server{SSH: protocol.SSHOff}, &protocol.ServerConfigResult{SSH: protocol.SSHOff, TailscaleIP: "100.64.0.9"}), components.StateLive, "", ""},
		{webLine(model.Server{}, &protocol.ServerConfigResult{Proxy: "direct"}), components.StateIdle, "", ""},
		{firewallLine(model.Server{Firewall: true}, nil), components.StateWaiting, "Waiting for the server", ""},
	} {
		if test.line.State != test.state || test.line.Note != test.note || test.line.Problem != test.problem {
			t.Errorf("%s: %s %q %q, want %s %q %q", test.line.Name, test.line.State, test.line.Note, test.line.Problem, test.state, test.note, test.problem)
		}
	}
}

// Without Cloudflare and Tailscale, SSH and the firewall still work: only
// the tunnel and SSH only through Tailscale wait, with a short link to
// Integrations. The page holds no key field.
func TestSecurityPageSendsKeysToIntegrations(t *testing.T) {
	var html strings.Builder
	view := SecurityView{Server: model.Server{ID: "0123456789abcdef0123456789abcdef", Name: "web-1", SSH: protocol.SSHEveryone, Security: `{"proxy":"direct","ssh":"everyone","firewall":false,"open_ports":[]}`}}
	if err := ServerSecurity(view).Render(projectPageContext("/app/tiny-rocket-co/servers/web-1/security"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{">Connect Cloudflare</a>", ">Connect Tailscale</a>", `href="/app/tiny-rocket-co/integrations"`, `data-security-line="ssh"`, `id="security-ssh-off"`, `data-security-line="firewall"`, `data-security-line="tunnel"`, `id="security-tunnel-off"`, ">Web<", webHint, sshHint, firewallHint} {
		if !strings.Contains(page, want) {
			t.Errorf("security page misses %q", want)
		}
	}
	for _, unwanted := range []string{"cloudflare_token", "tailscale_key", "Apply", `id="security-tunnel"`} {
		if strings.Contains(page, unwanted) {
			t.Errorf("security page still has %q", unwanted)
		}
	}
	if !strings.Contains(page, `disabled id="security-ssh-tailscale"`) {
		t.Error("SSH only through Tailscale can be picked without Tailscale")
	}
	if !strings.Contains(page, `disabled id="security-tunnel-on"`) {
		t.Error("the Cloudflare Tunnel can be picked without Cloudflare")
	}

	// With Tailscale connected the option opens and its link goes.
	html.Reset()
	view.Integrations.Tailscale = true
	if err := ServerSecurity(view).Render(projectPageContext("/app/tiny-rocket-co/servers/web-1/security"), &html); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html.String(), ">Connect Tailscale</a>") || strings.Contains(html.String(), `disabled id="security-ssh-tailscale"`) {
		t.Error("Tailscale is connected, but SSH only through Tailscale still waits for it")
	}
}

// Integrations: connected accounts with a green dot, a New key field
// that replaces the key, and Disconnect at the end; the others with their
// key field and Connect.
func TestIntegrationsPage(t *testing.T) {
	var html strings.Builder
	view := IntegrationsView{GitHub: true, GitHubAccounts: []model.GitHubInstallation{{Account: "acme"}}, Keys: service.Integrations{Cloudflare: true, CloudflareAccount: "acc1"}}
	if err := Integrations(view).Render(projectPageContext("/app/tiny-rocket-co/integrations"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{">Integrations</h1>", "acme", "Connect another account", "Account acc1", "Disconnect Cloudflare", `name="cloudflare_token"`, "New key", "Save", `name="tailscale_key"`, "Connect"} {
		if !strings.Contains(page, want) {
			t.Errorf("integrations page misses %q", want)
		}
	}
	if strings.Contains(page, "Disconnect Tailscale") {
		t.Error("a missing account can be disconnected")
	}
}
