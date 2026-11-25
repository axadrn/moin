package cli

import (
	"strings"
	"testing"
)

// servers secure sends all three settings and prints what is on the server,
// a line that differs with both.
func TestSecureSetsSSHAndShowsWhatDiffers(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/servers":             `{"items":[{"id":"s1","name":"web-1","online":true,"tunnel":false,"ssh":"everyone","firewall":true}]}`,
		"PUT /api/v1/servers/s1/security": `{"id":"s1","name":"web-1","online":true,"tunnel":false,"ssh":"off","firewall":true,"security":{"proxy":"direct","ssh":"everyone","firewall":true,"open_ports":["80/tcp","443/tcp"],"problems":["SSH: sshd still listens on port 22; stop it on the server"]}}`,
	}}
	code, stdout, stderr := runCLI(t, api, "servers", "secure", "web-1", "--ssh", "off", "--yes")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if !api.seen(`PUT /api/v1/servers/s1/security {"tunnel":false,"ssh":"off","firewall":true}`) {
		t.Fatalf("requests %v", api.requests)
	}
	for _, want := range []string{"everyone on the server ✗ moin says off", "open to everyone: 80, 443", "sshd still listens"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q misses %q", stdout, want)
		}
	}
	if code, _, _ := runCLI(t, api, "servers", "secure", "web-1", "--ssh", "open", "--yes"); code == 0 {
		t.Error("--ssh open passed")
	}
}

// servers secure without flags only shows: it sends nothing, so a read
// connection may run it.
func TestSecureWithoutFlagsOnlyShows(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/servers": `{"items":[{"id":"s1","name":"web-1","online":true,"tunnel":false,"ssh":"tailscale","firewall":true,"security":{"proxy":"direct","ssh":"tailscale","firewall":true,"open_ports":["80/tcp","443/tcp"]}}]}`,
	}}
	code, stdout, stderr := runCLI(t, api, "servers", "secure", "web-1")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	for _, request := range api.requests {
		if strings.HasPrefix(request, "PUT ") {
			t.Fatalf("it sent %s", request)
		}
	}
	if !strings.Contains(stdout, "web-1") || !strings.Contains(stdout, "open to everyone: 80, 443") {
		t.Errorf("stdout %q", stdout)
	}
}
