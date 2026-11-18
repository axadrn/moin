package daemon

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestServerAccess runs without Docker, on compose files in the shape
// docker compose config writes (access_docker_test.go checks that shape).
func TestServerAccess(t *testing.T) {
	appDir := "/var/lib/moin/apps/web"
	bind := func(source string, readOnly bool) map[string]any {
		return map[string]any{"services": map[string]any{"web": map[string]any{
			"volumes": []any{map[string]any{"type": "bind", "source": source, "target": "/x", "read_only": readOnly}},
		}}}
	}
	for _, test := range []struct {
		name   string
		config map[string]any
		want   string
	}{
		{"privileged", map[string]any{"services": map[string]any{"web": map[string]any{"privileged": true}}}, "service web: privileged: true"},
		{"docker socket", bind("/var/run/docker.sock", false), "mounts /var/run/docker.sock"},
		{"data in the app", bind(appDir+"/volumes/data", false), ""},
		{"next to the app", bind("/var/lib/moin/apps/web2/volumes", false), "mounts /var/lib/moin/apps/web2"},
		{"time zone", bind("/etc/localtime", true), ""},
		{"no services", map[string]any{}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checkServerAccess(nil, appDir, test.config)
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
			if err := checkServerAccess(&protocol.Rules{ServerAccess: true}, appDir, test.config); err != nil {
				t.Fatalf("server-access: on still refused: %v", err)
			}
			// Other rules do not allow it.
			if err := checkServerAccess(&protocol.Rules{ExecOff: true}, appDir, test.config); (err == nil) != (test.want == "") {
				t.Fatalf("with other rules: %v", err)
			}
		})
	}
}

func TestServerAccessRule(t *testing.T) {
	rules, err := parseRules("server-access: on\n", "rules")
	if err != nil || !rules.ServerAccess {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	if lines := rules.Lines(); len(lines) != 1 || lines[0] != "server-access: on" {
		t.Fatalf("lines = %v", lines)
	}
	if _, err := parseRules("server-access: yes\n", "rules"); err == nil {
		t.Fatal("took server-access: yes")
	}
}
