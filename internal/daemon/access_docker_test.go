//go:build docker

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// checkResolved reads compose through docker compose, as a deploy does, and
// checks it against the app's room, so the check sees what compose writes.
func checkResolved(t *testing.T, compose string, env map[string]string) (string, error) {
	t.Helper()
	ctx := context.Background()
	a := newApps(t.TempDir(), func(protocol.MessageType, string, any) {})
	src := filepath.Join(a.appDir("web"), "releases", "1", "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.setEnv(protocol.EnvSet{App: "web", Set: env}); err != nil {
		t.Fatal(err)
	}
	config, _, err := a.resolve(ctx, "web", src)
	if err != nil {
		return "", err
	}
	pinNames(config, "web", "web", func(volume string) string { return a.volumeName(ctx, "web", "", volume) })
	box := appRoom{
		dataDir:     resolved(filepath.Join(a.appDir("web"), "volumes")),
		releasesDir: resolved(filepath.Join(a.appDir("web"), "releases")),
		volumes:     a.volumeNames("web"),
		project:     "web",
		writable:    writableFolders(config),
	}
	return box.check(config), nil
}

// TestAppRoomCatalog passes every app of the catalog: they all stay in
// their room.
func TestAppRoomCatalog(t *testing.T) {
	files, _ := filepath.Glob("../../catalog/*/compose.yml")
	if len(files) == 0 {
		t.Fatal("no catalog")
	}
	vars := regexp.MustCompile(`\$\{([A-Z0-9_]+)`)
	for _, file := range files {
		t.Run(filepath.Base(filepath.Dir(file)), func(t *testing.T) {
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			env := map[string]string{}
			for _, match := range vars.FindAllStringSubmatch(string(body), -1) {
				env[match[1]] = "1"
			}
			reason, err := checkResolved(t, string(body), env)
			if err != nil {
				t.Fatal(err)
			}
			if reason != "" {
				t.Fatalf("refused: %s", reason)
			}
		})
	}
}

// TestAppRoomRefused refuses compose files that reach into the server, in
// the shape compose writes them.
func TestAppRoomRefused(t *testing.T) {
	for _, test := range []struct {
		name, service, top, want string
	}{
		{name: "privileged", service: "    privileged: true\n", want: "privileged is not allowed"},
		{name: "cap_add", service: "    cap_add: [SYS_ADMIN]\n", want: "cap_add is not allowed"},
		{name: "device", service: "    devices: [/dev/sda:/dev/sda]\n", want: "devices is not allowed"},
		{name: "device rules", service: "    device_cgroup_rules: ['b *:* rwm']\n", want: "device_cgroup_rules is not allowed"},
		{name: "host network", service: "    network_mode: host\n", want: "network_mode is not allowed"},
		{name: "host pid", service: "    pid: host\n", want: "pid is not allowed"},
		{name: "proxy's mounts", service: "    volumes_from: ['container:moin-traefik']\n", want: "volumes_from is not allowed"},
		{name: "unconfined", service: "    security_opt: [seccomp=unconfined]\n", want: "security_opt seccomp=unconfined"},
		{name: "hook", service: "    post_start:\n      - command: id\n        privileged: true\n", want: "post_start is not allowed"},
		{name: "docker socket", service: "    volumes: [/var/run/docker.sock:/var/run/docker.sock]\n", want: "mounts /var/run/docker.sock"},
		{name: "root folder", service: "    volumes: [/:/host:ro]\n", want: "mounts /, which is not a folder of the app"},
		{name: "the app folder", service: "    volumes: [../../..:/app]\n", want: "which is not a folder of the app"},
		{name: "source writable", service: "    volumes: [.:/app]\n", want: "of the source writable"},
		{name: "own subnet", service: "    networks: [inner]\n", top: "networks:\n  inner:\n    ipam:\n      config: [{subnet: 10.0.0.0/8}]\n", want: "ipam is not allowed"},
		{name: "time zone writable", service: "    volumes: [/etc/localtime:/etc/localtime]\n", want: "mounts /etc/localtime"},
		{name: "bind volume", service: "    volumes: [root:/x]\n", top: "volumes:\n  root:\n    driver_opts:\n      type: none\n      o: rbind\n      device: /\n", want: "driver_opts is not allowed"},
		{name: "disk volume", service: "    volumes: [disk:/x]\n", top: "volumes:\n  disk:\n    driver_opts:\n      type: ext4\n      device: /dev/sda1\n", want: "driver_opts is not allowed"},
		{name: "another app's volume", service: "    volumes: [data:/x]\n", top: "volumes:\n  data:\n    name: moin-shop_data\n", want: "not the app's"},
		{name: "another app's network", service: "    networks: [db]\n", top: "networks:\n  db:\n    name: shop_default\n    external: true\n", want: "joins shop_default"},
		{name: "secret file", service: "    secrets: [s]\n", top: "secrets:\n  s:\n    file: /var/run/docker.sock\n", want: "file is not allowed"},
		{name: "config file", service: "    configs: [c]\n", top: "configs:\n  c:\n    file: /etc/shadow\n", want: "file is not allowed"},
		{name: "build context", service: "    build:\n      context: /root\n", want: "context /root"},
		{name: "build contexts", service: "    build:\n      context: .\n      additional_contexts:\n        root: /\n", want: "additional_contexts is not allowed"},
		{name: "privileged build", service: "    build:\n      context: .\n      privileged: true\n", want: "privileged is not allowed"},
		{name: "host build network", service: "    build:\n      context: .\n      network: host\n", want: "network is not allowed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			compose := "services:\n  web:\n    image: nginx:1\n" + test.service + test.top
			reason, err := checkResolved(t, compose, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(reason, test.want) {
				t.Fatalf("reason = %q, want %q", reason, test.want)
			}
		})
	}
}

// TestSourceReadOnly lets a service see its source read-only.
func TestSourceReadOnly(t *testing.T) {
	reason, err := checkResolved(t, "services:\n  web:\n    image: nginx:1\n    volumes: [.:/app:ro, ./data:/data]\n", nil)
	if err != nil || reason != "" {
		t.Fatalf("refused the source read-only: %v %s", err, reason)
	}
}

// TestEnvFileOutsideSource refuses an env file of the server before compose
// reads it into the compose file, wherever compose would find it.
func TestEnvFileOutsideSource(t *testing.T) {
	for _, test := range []struct{ name, envFile string }{
		{"absolute", "/etc/hostname"},
		{"above the source", "../../../../../../../../etc/hostname"},
		{"home", "~/.bashrc"},
		{"through env", "${DIR}/hostname"},
		{"second document", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			compose := "services:\n  web:\n    image: nginx:1\n    env_file: [{path: '" + test.envFile + "', required: false}]\n"
			if test.envFile == "" {
				compose = "services:\n  web:\n    image: nginx:1\n---\nservices:\n  web:\n    env_file: [{path: /etc/hostname, required: false}]\n"
			}
			_, err := checkResolved(t, compose, map[string]string{"DIR": "/etc"})
			if err == nil || !strings.Contains(err.Error(), "is not in the app's source") && !strings.Contains(err.Error(), "takes a variable") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	reason, err := checkResolved(t, "services:\n  web:\n    image: nginx:1\n    env_file: [{path: app.env, required: false}]\n", nil)
	if err != nil || reason != "" {
		t.Fatalf("refused an env file of the source: %v %s", err, reason)
	}
}

// TestBuildTagsRefused: a build may not name its image like another one,
// such as the proxy's, which holds the Docker socket.
func TestBuildTagsRefused(t *testing.T) {
	reason, err := checkResolved(t, "services:\n  web:\n    build:\n      context: .\n      tags: ['traefik:v3.6']\n", nil)
	if err != nil || !strings.Contains(reason, "tags is not allowed") {
		t.Fatalf("reason = %q, err = %v", reason, err)
	}
}
