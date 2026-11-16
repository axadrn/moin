package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// fakeCloudflare answers the Cloudflare API calls moin makes and records
// them as "METHOD path".
type fakeCloudflare struct {
	mu    sync.Mutex
	calls []string
	body  map[string]string
	// noSettings refuses zone settings, as a token without Zone Settings
	// Edit gets.
	noSettings bool
}

func (f *fakeCloudflare) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		call := r.Method + " " + r.URL.Path
		f.calls = append(f.calls, call)
		f.body[call] = string(body)
		f.mu.Unlock()
		answer := func(result any) {
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
		}
		if r.Header.Get("Authorization") != "Bearer cf-good" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": "Invalid API Token"}}})
			return
		}
		switch {
		case call == "GET /accounts":
			answer([]map[string]string{{"id": "acc1"}})
		case call == "POST /accounts/acc1/cfd_tunnel":
			answer(map[string]string{"id": "tun1"})
		case call == "GET /accounts/acc1/cfd_tunnel/tun1/token":
			answer("tunnel-token-123456")
		case call == "PUT /accounts/acc1/cfd_tunnel/tun1/configurations", call == "DELETE /accounts/acc1/cfd_tunnel/tun1":
			answer(map[string]string{})
		case call == "GET /zones" && r.URL.Query().Get("name") == "blog.example.com":
			answer([]map[string]string{{"id": "zone1"}})
		case call == "GET /zones":
			answer([]map[string]string{})
		case call == "GET /zones/zone1/dns_records" && f.called("POST /zones/zone1/dns_records"):
			answer([]map[string]string{{"id": "rec1", "content": "tun1.cfargotunnel.com"}, {"id": "rec2", "content": "203.0.113.5"}})
		case call == "GET /zones/zone1/dns_records":
			answer([]map[string]string{})
		case call == "PATCH /zones/zone1/settings/always_use_https" && f.noSettings:
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": "Authentication error"}}})
		case call == "POST /zones/zone1/dns_records", call == "PUT /zones/zone1/dns_records/rec1", call == "DELETE /zones/zone1/dns_records/rec1", call == "PATCH /zones/zone1/settings/always_use_https":
			answer(map[string]string{"id": "rec1"})
		default:
			http.NotFound(w, r)
		}
	}))
}

func (f *fakeCloudflare) called(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, made := range f.calls {
		if made == call {
			return true
		}
	}
	return false
}

// A token with only Cloudflare Tunnel Edit and DNS Edit lists no account;
// its zones name the account.
func TestCloudflareAccountFromZones(t *testing.T) {
	cloudflare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any = []any{}
		if r.URL.Path == "/zones" {
			result = []map[string]any{{"id": "zone1", "account": map[string]string{"id": "acc2"}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer cloudflare.Close()
	account, err := NewCloudflare(cloudflare.URL).Account(context.Background(), "cf-tunnel-only")
	if err != nil || account != "acc2" {
		t.Fatalf("account = %q, %v", account, err)
	}
}

// SSH kept to Tailscale or off without the firewall is a leftover too: a
// daemon that comes back after it was set to everyone must open it again.
// A tailnet alone is none: it may be joined by hand.
func TestLeftOver(t *testing.T) {
	for _, report := range []protocol.ServerConfigResult{{Proxy: "direct", Firewall: true}, {Proxy: "tunnel"}, {Proxy: "direct", SSH: protocol.SSHTailscale}, {Proxy: "direct", SSH: protocol.SSHOff}} {
		if !leftOver(&report) {
			t.Errorf("leftOver(%+v) = false", report)
		}
	}
	if leftOver(nil) || leftOver(&protocol.ServerConfigResult{Proxy: "direct", SSH: protocol.SSHEveryone, TailscaleIP: "100.64.0.9"}) {
		t.Error("nothing set up is no leftover")
	}
}

func TestServerSecurity(t *testing.T) {
	apps, _, server, daemon := newAppService(t)
	fake := &fakeCloudflare{body: map[string]string{}}
	cloudflare := fake.serve()
	defer cloudflare.Close()
	apps.SetCloudflare(NewCloudflare(cloudflare.URL))
	ctx := context.Background()

	_, err := apps.SetServerSecurity(ctx, owner, "web-1", SecurityInput{Tunnel: true, SSH: protocol.SSHEveryone})
	if !errors.Is(err, ErrNoCloudflare) {
		t.Fatalf("tunnel without a token: err = %v", err)
	}
	_, err = apps.SetServerSecurity(ctx, owner, "web-1", SecurityInput{SSH: protocol.SSHTailscale})
	if !errors.Is(err, ErrNoTailscale) {
		t.Fatalf("SSH on Tailscale without a key: err = %v", err)
	}
	_, err = apps.SetServerSecurity(ctx, owner, "web-1", SecurityInput{SSH: "nobody"})
	if !errors.Is(err, ErrSSH) {
		t.Fatalf("SSH for nobody: err = %v", err)
	}
	bad := "cf-bad"
	if err := apps.SetIntegrations(ctx, owner, &bad, nil); !errors.Is(err, ErrCloudflareToken) {
		t.Fatalf("a token Cloudflare refuses: err = %v", err)
	}
	wrongKey := "nope"
	if err := apps.SetIntegrations(ctx, owner, nil, &wrongKey); !errors.Is(err, ErrTailscaleKey) {
		t.Fatalf("a key that is no Tailscale key: err = %v", err)
	}
	good, key := "cf-good", "tskey-auth-abc"
	err = apps.SetIntegrations(ctx, owner, &good, &key)
	if err != nil {
		t.Fatal(err)
	}
	integrations, _ := apps.Integrations(owner.WorkspaceID)
	if !integrations.Cloudflare || integrations.CloudflareAccount != "acc1" || !integrations.Tailscale {
		t.Fatalf("integrations = %+v", integrations)
	}

	app, err := apps.Create(owner, "blog", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = apps.SetDomains(owner, app.ID, "", []string{"blog.example.com"}, "")
	if err != nil {
		t.Fatal(err)
	}

	// The daemon answers every server config.
	done := make(chan struct{})
	defer close(done)
	go func() {
		answered := map[string]bool{}
		for {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
			}
			daemon.mu.Lock()
			pending := []protocol.Message{}
			for _, message := range daemon.sent {
				if message.Type == protocol.TypeServerConfig && !answered[message.RequestID] {
					pending = append(pending, message)
					answered[message.RequestID] = true
				}
			}
			daemon.mu.Unlock()
			for _, request := range pending {
				var config protocol.ServerConfig
				request.Decode(&config)
				proxy := "direct"
				if config.TunnelToken != "" {
					proxy = "tunnel"
				}
				answer, _ := protocol.NewMessage(protocol.TypeServerConfigResult, protocol.ServerConfigResult{Proxy: proxy, Firewall: config.Firewall, OpenPorts: []string{"41641/udp"}, TailscaleIP: "100.64.0.9"})
				answer.RequestID = request.RequestID
				apps.servers.Receive(server.ID, answer)
			}
		}
	}()
	secured, err := apps.SetServerSecurity(ctx, owner, "web-1", SecurityInput{Tunnel: true, SSH: protocol.SSHTailscale, Firewall: true})
	if err != nil {
		t.Fatal(err)
	}
	if !secured.Tunnel || secured.TunnelID != "tun1" || !secured.Firewall || secured.SSH != protocol.SSHTailscale {
		t.Fatalf("server = %+v", secured)
	}
	// The token stays while a tunnel needs it to be deleted.
	none := ""
	if err := apps.SetIntegrations(ctx, owner, &none, nil); !errors.Is(err, ErrTunnelsOn) {
		t.Fatalf("remove the token with a tunnel on: err = %v", err)
	}
	report := SecurityReport(*secured)
	if report == nil || report.Proxy != "tunnel" || report.TailscaleIP != "100.64.0.9" {
		t.Fatalf("report = %+v", report)
	}
	var config protocol.ServerConfig
	daemon.last(t, protocol.TypeServerConfig, &config)
	if config.TunnelToken != "tunnel-token-123456" || config.TailscaleKey != "tskey-auth-abc" || config.SSH != protocol.SSHTailscale || !config.SSHOnTailscale || !config.Firewall {
		t.Fatalf("daemon config = %+v", config)
	}
	for _, call := range []string{"POST /accounts/acc1/cfd_tunnel", "PUT /accounts/acc1/cfd_tunnel/tun1/configurations", "POST /zones/zone1/dns_records", "PATCH /zones/zone1/settings/always_use_https"} {
		if !fake.called(call) {
			t.Errorf("Cloudflare never got %s: %v", call, fake.calls)
		}
	}
	ingress := fake.body["PUT /accounts/acc1/cfd_tunnel/tun1/configurations"]
	if !strings.Contains(ingress, `"hostname":"blog.example.com"`) || !strings.Contains(ingress, "http://moin-traefik:8080") || !strings.Contains(ingress, "http_status:404") {
		t.Fatalf("ingress = %s", ingress)
	}
	if !strings.Contains(fake.body["POST /zones/zone1/dns_records"], `"content":"tun1.cfargotunnel.com"`) {
		t.Fatalf("dns record = %s", fake.body["POST /zones/zone1/dns_records"])
	}
	checks, err := apps.CheckDomains(ctx, owner.WorkspaceID, "blog")
	if err != nil || checks[0].Address != "tun1.cfargotunnel.com" {
		t.Fatalf("domain check in tunnel mode = %+v, %v", checks, err)
	}

	// Without Zone Settings Edit the DNS still points at the tunnel; only
	// Always Use HTTPS is left to do by hand.
	fake.mu.Lock()
	fake.noSettings = true
	fake.mu.Unlock()
	problems, err := apps.syncTunnel(ctx, secured)
	if err != nil || len(problems) != 1 || !strings.HasPrefix(problems[0], "Always Use HTTPS for blog.example.com") {
		t.Fatalf("problems without Zone Settings Edit = %q, %v", problems, err)
	}

	open, err := apps.SetServerSecurity(ctx, owner, "web-1", SecurityInput{SSH: protocol.SSHEveryone})
	if err != nil {
		t.Fatal(err)
	}
	if open.Tunnel || open.TunnelID != "" || !fake.called("DELETE /accounts/acc1/cfd_tunnel/tun1") {
		t.Fatalf("turning the tunnel off: %+v, calls %v", open, fake.calls)
	}
	if !fake.called("DELETE /zones/zone1/dns_records/rec1") || fake.called("DELETE /zones/zone1/dns_records/rec2") {
		t.Fatalf("turning the tunnel off deletes only the record at the tunnel: %v", fake.calls)
	}
	if err := apps.SetIntegrations(ctx, owner, &none, nil); err != nil {
		t.Fatalf("remove the token with the tunnels off: %v", err)
	}
	if integrations, _ := apps.Integrations(owner.WorkspaceID); integrations.Cloudflare {
		t.Fatalf("the token is still there: %+v", integrations)
	}
}

// A daemon reports by itself what really is and what it put back: the
// report is kept, each fix is in the activity as moin, and a tailnet joined
// by hand lets SSH go only there without a Tailscale key.
func TestDaemonReportsSecurity(t *testing.T) {
	apps, _, server, _ := newAppService(t)
	report := protocol.ServerConfigResult{Proxy: "direct", TailscaleIP: "100.64.0.7", SSH: protocol.SSHEveryone, OpenPorts: []string{}, Fixed: []string{"the firewall"}}
	message, _ := protocol.NewMessage(protocol.TypeServerSecurity, report)
	apps.servers.Receive(server.ID, message)
	stored, err := apps.servers.servers.ByIDOrNameForWorkspace(server.ID, owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	seen := SecurityReport(*stored)
	if seen == nil || seen.TailscaleIP != "100.64.0.7" || len(seen.Fixed) != 0 {
		t.Fatalf("stored report = %+v", seen)
	}
	events, err := apps.servers.activity.ListPage(owner.WorkspaceID, model.ActivityFilter{}, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	fixed := 0
	for _, event := range events {
		if event.Action == "server.security_fixed" && event.ActorUserID == nil {
			fixed++
		}
	}
	if fixed != 1 {
		t.Fatalf("fixes in the activity as moin = %d", fixed)
	}
	if _, err := apps.SetServerSecurity(context.Background(), owner, server.ID, SecurityInput{SSH: protocol.SSHTailscale}); err != nil {
		t.Fatalf("SSH only through a tailnet joined by hand: %v", err)
	}
}

// A daemon from before it reported SSH said it only through its tailnet IP.
func TestSecurityReportOfAnOldDaemon(t *testing.T) {
	for _, test := range []struct {
		ssh, report, want string
	}{
		{protocol.SSHTailscale, `{"proxy":"direct","tailscale_ip":"100.64.0.9","firewall":false,"open_ports":[]}`, protocol.SSHTailscale},
		{protocol.SSHTailscale, `{"proxy":"direct","firewall":false,"open_ports":[],"problems":["SSH stays open: Tailscale is not up"]}`, protocol.SSHEveryone},
		{protocol.SSHOff, `{"proxy":"direct","firewall":false,"open_ports":[]}`, protocol.SSHEveryone},
	} {
		got := SecurityReport(model.Server{SSH: test.ssh, Security: test.report})
		if got == nil || got.SSH != test.want {
			t.Errorf("set %s, report %s: ssh = %+v, want %s", test.ssh, test.report, got, test.want)
		}
	}
}
