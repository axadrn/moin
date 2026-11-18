//go:build docker

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestServerAccessRefused reads each compose file through docker compose,
// as a deploy does, so the check sees what compose really writes.
func TestServerAccessRefused(t *testing.T) {
	for _, test := range []struct {
		name, service, want string
	}{
		{name: "privileged", service: "    privileged: true\n", want: "privileged: true"},
		{name: "cap_add", service: "    cap_add: [SYS_ADMIN]\n", want: "cap_add"},
		{name: "device", service: "    devices: [/dev/sda:/dev/sda]\n", want: "devices"},
		{name: "host network", service: "    network_mode: host\n", want: "network_mode: host"},
		{name: "host pid", service: "    pid: host\n", want: "pid: host"},
		{name: "host ipc", service: "    ipc: host\n", want: "ipc: host"},
		{name: "host userns", service: "    userns_mode: host\n", want: "userns_mode: host"},
		{name: "unconfined", service: "    security_opt: [seccomp=unconfined]\n", want: "security_opt"},
		{name: "apparmor off", service: "    security_opt: [\"apparmor:unconfined\"]\n", want: "security_opt"},
		{name: "docker socket", service: "    volumes: [/var/run/docker.sock:/var/run/docker.sock]\n", want: "mounts /var/run/docker.sock"},
		{name: "root folder", service: "    volumes: [/:/host:ro]\n", want: "mounts / "},
		{name: "folder above the app", service: "    volumes: [../../../../etc:/x]\n", want: "mounts "},
		{name: "time zone writable", service: "    volumes: [/etc/localtime:/etc/localtime]\n", want: "mounts /etc/localtime"},
		{name: "privileged build", service: "    build:\n      context: .\n      privileged: true\n", want: "build: privileged"},
		{name: "host build network", service: "    build:\n      context: .\n      network: host\n", want: "build: network: host"},
		{name: "bind volume", service: "    volumes: [root:/x]\nvolumes:\n  root:\n    driver_opts:\n      type: none\n      o: bind\n      device: /\n", want: "volume root"},
		{name: "plain app", service: "    volumes: [./data:/data, cache:/cache, /etc/localtime:/etc/localtime:ro]\n    cap_drop: [ALL]\n    security_opt: [no-new-privileges:true]\nvolumes:\n  cache: {}\n", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			a := newApps(dir, func(protocol.MessageType, string, any) {})
			src := filepath.Join(a.appDir("access"), "releases", "1", "src")
			if err := os.MkdirAll(src, 0o755); err != nil {
				t.Fatal(err)
			}
			compose := "services:\n  web:\n    image: nginx:1\n" + test.service
			if err := os.WriteFile(filepath.Join(src, "compose.yml"), []byte(compose), 0o644); err != nil {
				t.Fatal(err)
			}
			config, _, err := a.resolve(context.Background(), "access", src, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = checkServerAccess(nil, a.appDir("access"), config)
			if test.want == "" {
				if err != nil {
					t.Fatalf("refused a plain app: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "server-access: on") {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
			if err := checkServerAccess(&protocol.Rules{ServerAccess: true}, a.appDir("access"), config); err != nil {
				t.Fatalf("server-access: on still refused: %v", err)
			}
		})
	}
}
