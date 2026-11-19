package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAppRoom runs without Docker, on compose files in the shape docker
// compose config writes (access_docker_test.go checks that shape with real
// compose files).
func TestAppRoom(t *testing.T) {
	appDir := t.TempDir()
	data := filepath.Join(appDir, "volumes", "data")
	src := filepath.Join(appDir, "releases", "2", "src")
	for _, dir := range []string{data, src, filepath.Join(appDir, "volumes", "data", "db")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "nginx.conf"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A link a container wrote into its data folder.
	if err := os.Symlink("/", filepath.Join(data, "link")); err != nil {
		t.Fatal(err)
	}
	box := appRoom{
		dataDir:     filepath.Join(appDir, "volumes"),
		releasesDir: filepath.Join(appDir, "releases"),
		volumes:     []string{"moin-web_cache"},
		project:     "web",
	}
	service := func(settings map[string]any) map[string]any {
		return map[string]any{"services": map[string]any{"web": settings}}
	}
	bind := func(source string, readOnly bool) map[string]any {
		return service(map[string]any{"image": "nginx", "volumes": []any{
			map[string]any{"type": "bind", "source": source, "target": "/x", "read_only": readOnly},
		}})
	}
	for _, test := range []struct {
		name   string
		config map[string]any
		want   string
	}{
		// Allowed: what a normal app uses.
		{"plain app", service(map[string]any{
			"image": "nginx", "environment": map[string]any{"A": "1"}, "restart": "unless-stopped",
			"healthcheck": map[string]any{"test": []any{"CMD", "true"}}, "networks": map[string]any{"default": nil},
			"security_opt": []any{"no-new-privileges:true"}, "cap_drop": []any{"ALL"}, "x-note": "mine",
		}), ""},
		{"data folder", bind(data, false), ""},
		{"file of the source", bind(filepath.Join(src, "nginx.conf"), true), ""},
		{"time zone read-only", bind("/etc/localtime", true), ""},
		{"named volume", map[string]any{
			"services": map[string]any{"web": map[string]any{"image": "nginx", "volumes": []any{
				map[string]any{"type": "volume", "source": "cache", "target": "/cache"},
			}}},
			"volumes": map[string]any{"cache": map[string]any{"name": "moin-web_cache"}},
		}, ""},
		{"app network and the proxy", map[string]any{"services": map[string]any{"web": map[string]any{"image": "nginx"}},
			"networks": map[string]any{
				"default":    map[string]any{"name": "web_default"},
				proxyNetwork: map[string]any{"name": proxyNetwork, "external": true},
			}}, ""},
		{"build from the source", service(map[string]any{"build": map[string]any{
			"context": src, "dockerfile": "../railpack-plan.json", "args": map[string]any{"A": "1"},
		}}), ""},
		{"secret from env", map[string]any{"services": map[string]any{"web": map[string]any{"image": "nginx"}},
			"secrets": map[string]any{"token": map[string]any{"environment": "TOKEN"}}}, ""},

		// Refused: settings off the list.
		{"privileged", service(map[string]any{"image": "nginx", "privileged": true}), "privileged is not allowed"},
		{"cap_add", service(map[string]any{"cap_add": []any{"SYS_ADMIN"}}), "cap_add is not allowed"},
		{"devices", service(map[string]any{"devices": []any{"/dev/sda"}}), "devices is not allowed"},
		{"device rules", service(map[string]any{"device_cgroup_rules": []any{"b *:* rwm"}}), "device_cgroup_rules is not allowed"},
		{"host network", service(map[string]any{"network_mode": "host"}), "network_mode is not allowed"},
		{"another container's mounts", service(map[string]any{"volumes_from": []any{"container:moin-traefik"}}), "volumes_from is not allowed"},
		{"hook", service(map[string]any{"post_start": []any{map[string]any{"privileged": true}}}), "post_start is not allowed"},
		{"label file", service(map[string]any{"label_file": []any{"/etc/shadow"}}), "label_file is not allowed"},
		{"seccomp off", service(map[string]any{"security_opt": []any{"seccomp=unconfined"}}), "security_opt seccomp=unconfined"},
		{"kernel sysctl", service(map[string]any{"sysctls": map[string]any{"kernel.core_pattern": "x"}}), "sysctl kernel.core_pattern"},
		{"gpu", service(map[string]any{"deploy": map[string]any{"resources": map[string]any{"reservations": map[string]any{"devices": []any{map[string]any{}}}}}}), "devices of the server"},
		{"unknown top key", map[string]any{"include": []any{"x"}}, "include is not allowed"},

		// Refused: mounts out of the room.
		{"docker socket", bind("/var/run/docker.sock", false), "mounts /var/run/docker.sock"},
		{"root", bind("/", true), "mounts / of the server"},
		{"time zone writable", bind("/etc/localtime", false), "mounts /etc/localtime"},
		{"the app folder", bind(appDir, false), "of the server"},
		{"all data folders", bind(filepath.Join(appDir, "volumes"), false), "of the server"},
		{"a release with its compose file", bind(filepath.Join(appDir, "releases", "2"), false), "of the server"},
		{"link to the disk", bind(filepath.Join(data, "link"), false), "of the server"},
		{"bind propagation", service(map[string]any{"volumes": []any{
			map[string]any{"type": "bind", "source": data, "target": "/x", "bind": map[string]any{"propagation": "rshared"}},
		}}), "propagation is not allowed"},
		{"mount type", service(map[string]any{"volumes": []any{map[string]any{"type": "npipe", "source": "x", "target": "/x"}}}), "type npipe"},

		// Refused: other apps' volumes and networks, files of the server.
		{"another app's volume", map[string]any{"volumes": map[string]any{"data": map[string]any{"name": "moin-shop_data"}}}, "not the app's"},
		{"disk as a volume", map[string]any{"volumes": map[string]any{"cache": map[string]any{"name": "moin-web_cache", "driver_opts": map[string]any{"device": "/dev/sda1"}}}}, "driver_opts is not allowed"},
		{"external volume", map[string]any{"volumes": map[string]any{"cache": map[string]any{"name": "moin-web_cache", "external": true}}}, "external is not allowed"},
		{"another app's network", map[string]any{"networks": map[string]any{"db": map[string]any{"name": "shop_default", "external": true}}}, "joins shop_default"},
		{"network named like another", map[string]any{"networks": map[string]any{"default": map[string]any{"name": "shop_default"}}}, "not the app's"},
		{"macvlan", map[string]any{"networks": map[string]any{"default": map[string]any{"name": "web_default", "driver": "macvlan"}}}, "driver macvlan"},
		{"secret from a file", map[string]any{"secrets": map[string]any{"s": map[string]any{"file": "/var/run/docker.sock"}}}, "file is not allowed"},
		{"config from a file", map[string]any{"configs": map[string]any{"c": map[string]any{"file": "/etc/shadow"}}}, "file is not allowed"},
		{"build context of the server", service(map[string]any{"build": map[string]any{"context": "/root"}}), "context /root"},
		{"build contexts", service(map[string]any{"build": map[string]any{"context": src, "additional_contexts": map[string]any{"x": "/"}}}), "additional_contexts is not allowed"},
		{"build ssh", service(map[string]any{"build": map[string]any{"context": src, "ssh": []any{"default"}}}), "ssh is not allowed"},
		{"privileged build", service(map[string]any{"build": map[string]any{"context": src, "privileged": true}}), "privileged is not allowed"},
		{"Dockerfile of the server", service(map[string]any{"build": map[string]any{"context": src, "dockerfile": "/etc/shadow"}}), "Dockerfile /etc/shadow"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reason := box.check(test.config)
			if test.want == "" && reason != "" || test.want != "" && !strings.Contains(reason, test.want) {
				t.Fatalf("reason = %q, want %q", reason, test.want)
			}
		})
	}
}

// TestAppRoomNested refuses a mount inside a folder a container can write,
// which the container could swap for a link while the deploy starts.
func TestAppRoomNested(t *testing.T) {
	appDir := t.TempDir()
	data := filepath.Join(appDir, "volumes", "data")
	if err := os.MkdirAll(filepath.Join(data, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	box := appRoom{dataDir: filepath.Join(appDir, "volumes"), releasesDir: filepath.Join(appDir, "releases")}
	inner := map[string]any{"services": map[string]any{"web": map[string]any{"volumes": []any{
		map[string]any{"type": "bind", "source": filepath.Join(data, "db"), "target": "/db"},
	}}}}
	if reason := box.check(inner); reason != "" {
		t.Fatalf("refused alone: %s", reason)
	}
	// The running release writes data.
	box.writable = []string{data}
	if reason := box.check(inner); !strings.Contains(reason, "which a container can write") {
		t.Fatalf("reason = %q", reason)
	}
	// Mounting the same folder again is fine.
	box.writable = []string{filepath.Join(data, "db")}
	if reason := box.check(inner); reason != "" {
		t.Fatalf("refused the same folder: %s", reason)
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
