package daemon

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/axadrn/moin/protocol"
)

// timeFiles are the host files a container may read without server-access:
// the server's time zone, which many images mount.
var timeFiles = []string{"/etc/localtime", "/etc/timezone"}

// checkServerAccess refuses a compose file whose containers reach into the
// server, unless the rules allow it with server-access: on. Each of these
// gives a container root over the server, so whoever controls the hub
// could take the server with one deploy. config is the compose file as
// `docker compose config` wrote it, with the data mounts moved to appDir.
func checkServerAccess(rules *protocol.Rules, appDir string, config map[string]any) error {
	if rules != nil && rules.ServerAccess {
		return nil
	}
	reason := serverAccess(appDir, config)
	if reason == "" {
		return nil
	}
	return fmt.Errorf("%s, which reaches into the server. moin refuses it unless %s has server-access: on", reason, RulesFile())
}

// serverAccess names the first setting of config that reaches into the
// server, "" when there is none.
func serverAccess(appDir string, config map[string]any) string {
	services, _ := config["services"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(services)) {
		service, _ := services[name].(map[string]any)
		if reason := serviceAccess(appDir, service); reason != "" {
			return "service " + name + ": " + reason
		}
	}
	volumes, _ := config["volumes"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(volumes)) {
		volume, _ := volumes[name].(map[string]any)
		options, _ := volume["driver_opts"].(map[string]any)
		if o, _ := options["o"].(string); slices.Contains(strings.Split(o, ","), "bind") {
			return "volume " + name + ": binds a folder of the server"
		}
	}
	return ""
}

func serviceAccess(appDir string, service map[string]any) string {
	if service["privileged"] == true {
		return "privileged: true"
	}
	if caps, _ := service["cap_add"].([]any); len(caps) > 0 {
		return fmt.Sprintf("cap_add: %v", caps)
	}
	if devices, _ := service["devices"].([]any); len(devices) > 0 {
		return "devices of the server"
	}
	for _, key := range []string{"network_mode", "pid", "ipc", "userns_mode", "cgroup"} {
		if service[key] == "host" {
			return key + ": host"
		}
	}
	options, _ := service["security_opt"].([]any)
	for _, item := range options {
		option, _ := item.(string)
		if strings.Contains(option, "unconfined") || strings.Contains(option, "disable") {
			return "security_opt: " + option
		}
	}
	mounts, _ := service["volumes"].([]any)
	for _, item := range mounts {
		mount, _ := item.(map[string]any)
		source, _ := mount["source"].(string)
		if mount["type"] != "bind" || within(appDir, source) {
			continue
		}
		if slices.Contains(timeFiles, source) && mount["read_only"] == true {
			continue
		}
		return "mounts " + source + " of the server"
	}
	build, _ := service["build"].(map[string]any)
	if build["privileged"] == true {
		return "build: privileged: true"
	}
	if build["network"] == "host" {
		return "build: network: host"
	}
	return ""
}
