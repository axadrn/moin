package daemon

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// An app runs in its own room on the server: its containers, its data
// folders, its named volumes and its networks. A compose file may only use
// the settings below, so whoever controls the hub cannot deploy a container
// that reaches into the server. Anything not listed is refused, also a
// setting Docker adds later, unless the server's rules have
// server-access: on, which only root on the server can write.

// serviceKeys are the settings a service may use. Those marked "checked"
// have their value checked in checkService.
var serviceKeys = map[string]string{
	"image":             "which image runs",
	"build":             "checked: builds from the release's source",
	"command":           "what the container runs",
	"entrypoint":        "what the container runs",
	"environment":       "its env vars",
	"working_dir":       "its folder inside the container",
	"user":              "the user inside the container",
	"healthcheck":       "how moin sees it is up",
	"depends_on":        "which services start first",
	"restart":           "start again after a crash",
	"labels":            "labels, such as the proxy's",
	"networks":          "the app's own networks",
	"links":             "other services of the app",
	"ports":             "ports it opens",
	"expose":            "ports for the app's other services",
	"volumes":           "checked: its data folders and named volumes",
	"tmpfs":             "folders in memory",
	"configs":           "the app's configs",
	"secrets":           "the app's secrets",
	"hostname":          "its name in the app's network",
	"extra_hosts":       "extra names in its /etc/hosts",
	"dns":               "its DNS servers",
	"dns_search":        "its DNS search domains",
	"ulimits":           "limits such as open files",
	"init":              "a small init that reaps processes",
	"read_only":         "a read-only root file system",
	"tty":               "a terminal",
	"stdin_open":        "an open stdin",
	"stop_grace_period": "how long it may take to stop",
	"stop_signal":       "the signal that stops it",
	"shm_size":          "the size of /dev/shm",
	"mem_limit":         "memory limit",
	"mem_reservation":   "memory reservation",
	"memswap_limit":     "memory and swap limit",
	"cpus":              "CPU limit",
	"cpu_shares":        "CPU weight",
	"pids_limit":        "process limit",
	"deploy":            "checked: replicas and resource limits",
	"cap_drop":          "fewer powers, never more",
	"security_opt":      "checked: only no-new-privileges",
	"sysctls":           "checked: only the container's own net.* settings",
	"platform":          "which CPU the image is for",
	"pull_policy":       "when to pull the image",
	"scale":             "how many containers",
}

// buildKeys are the settings a build may use. Not tags: a tag like
// traefik:v3.6 would replace the image of moin's own proxy, which holds the
// Docker socket.
var buildKeys = map[string]string{
	"context":           "checked: a folder of the release's source",
	"dockerfile":        "checked: a file of the release, such as Railpack's plan",
	"dockerfile_inline": "a Dockerfile written in the compose file",
	"args":              "build arguments",
	"target":            "the stage to build",
	"secrets":           "the app's secrets, for the build",
	"labels":            "labels of the image",
	"platforms":         "which CPUs to build for",
	"shm_size":          "the size of /dev/shm",
	"extra_hosts":       "extra names in /etc/hosts",
	"pull":              "pull the base image",
	"no_cache":          "build without the cache",
}

// mountKeys are the settings of one mount in volumes.
var mountKeys = map[string]string{
	"type":        "checked: bind, volume or tmpfs",
	"source":      "checked: a data folder, the release's source or a volume of the app",
	"target":      "where the container sees it",
	"read_only":   "read-only",
	"bind":        "checked: only create_host_path and selinux",
	"volume":      "nocopy and subpath",
	"tmpfs":       "size and mode",
	"consistency": "a hint for Docker Desktop",
}

// topKeys are the settings of the compose file itself. check checks each of
// networks, volumes, configs and secrets.
var topKeys = map[string]string{
	"name":     "the project's name",
	"services": "the services",
	"networks": "checked: the app's own networks, and the proxy's",
	"volumes":  "checked: the app's own named volumes",
	"configs":  "checked: written in the compose file or from env",
	"secrets":  "checked: written in the compose file or from env",
}

// timeFiles are the host files a container may read: the server's time
// zone, which many images mount read-only.
var timeFiles = []string{"/etc/localtime", "/etc/timezone"}

// appRoom is what one deploy of an app may reach.
type appRoom struct {
	// dataDir holds the app's data folders.
	dataDir string
	// releasesDir holds the releases; only their src folders may be mounted.
	releasesDir string
	// volumes are the names of the app's named volumes.
	volumes []string
	// project names the app's networks: <project>_<network>.
	project string
	// mounts are the folders of the server its rules allow, with mounts:.
	mounts []string
	// writable are the folders a running container can write, of this
	// release and of the one running now, through their links. A mount
	// inside one of them could be swapped for a link to anywhere while the
	// deploy starts.
	writable []string
}

// checkCompose refuses a release whose compose file reaches beyond its
// app, unless the server's rules allow it with server-access: on. It reads
// the compose file as it will run, after moin filled it in, so it holds for
// every deploy: new source, an image, new env or a rollback.
func (a *apps) checkCompose(release *release) error {
	if release.rules != nil && release.rules.ServerAccess {
		return nil
	}
	config, err := readJSON(release.composeFile)
	if err != nil {
		return err
	}
	room := a.roomOf(release, config)
	if reason := room.check(config); reason != "" {
		return fmt.Errorf("%s: this reaches beyond the app, so the server refuses it. Allow it with server-access: on in %s", reason, RulesFile())
	}
	return nil
}

// roomOf is what release may reach: the app's folders and volumes, the
// folders its rules allow, and the folders its containers write now.
func (a *apps) roomOf(release *release, config map[string]any) appRoom {
	app := release.start.App
	room := appRoom{
		dataDir:     resolved(filepath.Join(a.appDir(app), "volumes")),
		releasesDir: resolved(filepath.Join(a.appDir(app), "releases")),
		volumes:     a.volumeNames(app),
		project:     nameOf(release.start),
	}
	if release.rules != nil {
		for _, folder := range release.rules.Mounts {
			room.mounts = append(room.mounts, resolved(folder))
		}
	}
	if current := a.current(app); current > 0 && current != release.start.Number {
		running, _ := readJSON(filepath.Join(a.releaseDir(app, current), "compose.json"))
		room.writable = writableFolders(running)
	}
	room.writable = append(room.writable, writableFolders(config)...)
	return room
}

// check names the first setting of config outside the room, "" when
// there is none.
func (r appRoom) check(config map[string]any) string {
	for _, key := range sortedKeys(config) {
		if _, ok := topKeys[key]; !ok && !strings.HasPrefix(key, "x-") {
			return key + " is not allowed"
		}
	}
	networks, _ := config["networks"].(map[string]any)
	for _, key := range sortedKeys(networks) {
		if reason := r.checkNetwork(key, networks[key]); reason != "" {
			return "network " + key + ": " + reason
		}
	}
	volumes, _ := config["volumes"].(map[string]any)
	for _, key := range sortedKeys(volumes) {
		if reason := r.checkVolume(volumes[key]); reason != "" {
			return "volume " + key + ": " + reason
		}
	}
	for _, kind := range []string{"configs", "secrets"} {
		items, _ := config[kind].(map[string]any)
		for _, key := range sortedKeys(items) {
			// From the compose file or from env, never from a file.
			if reason := onlyKeys(items[key], "name", "content", "environment", "labels"); reason != "" {
				return kind + " " + key + ": " + reason
			}
		}
	}
	services, _ := config["services"].(map[string]any)
	for _, name := range sortedKeys(services) {
		service, _ := services[name].(map[string]any)
		if reason := r.checkService(service); reason != "" {
			return "service " + name + ": " + reason
		}
	}
	return ""
}

// checkNetwork allows the app's own networks, named as moin names them, and
// the proxy's.
func (r appRoom) checkNetwork(key string, value any) string {
	network, _ := value.(map[string]any)
	name, _ := network["name"].(string)
	if reason := onlyKeys(network, "name", "external", "driver", "internal", "attachable", "labels", "enable_ipv6", "ipam"); reason != "" {
		return reason
	}
	// compose writes an empty ipam. A subnet of the app's own could cover
	// the server's network and take its traffic; Docker picks a free one.
	if ipam, _ := network["ipam"].(map[string]any); len(ipam) > 0 {
		return "ipam is not allowed"
	}
	if network["driver"] != nil && network["driver"] != "bridge" {
		return fmt.Sprintf("driver %v", network["driver"])
	}
	// The only network of another project an app joins is the proxy's.
	if network["external"] == true {
		if name != proxyNetwork {
			return "joins " + name + ", which is not the app's"
		}
		return ""
	}
	if name != r.project+"_"+key {
		return "the name " + name + " is not the app's"
	}
	return ""
}

// checkVolume allows the app's own named volumes, with no driver: a driver
// option could mount a disk or a folder of the server.
func (r appRoom) checkVolume(value any) string {
	volume, _ := value.(map[string]any)
	if reason := onlyKeys(volume, "name", "labels"); reason != "" {
		return reason
	}
	if name, _ := volume["name"].(string); !slices.Contains(r.volumes, name) {
		return "the name " + name + " is not the app's"
	}
	return ""
}

// checkService checks one service: its settings are on the list, and those
// that point at the server stay in the room.
func (r appRoom) checkService(service map[string]any) string {
	for _, key := range sortedKeys(service) {
		if _, ok := serviceKeys[key]; !ok && !strings.HasPrefix(key, "x-") {
			return key + " is not allowed"
		}
	}
	if build, ok := service["build"].(map[string]any); ok {
		if reason := r.checkBuild(build); reason != "" {
			return "build: " + reason
		}
	}
	mounts, _ := service["volumes"].([]any)
	for _, item := range mounts {
		mount, _ := item.(map[string]any)
		if reason := r.checkMount(mount); reason != "" {
			return reason
		}
	}
	deploy, _ := service["deploy"].(map[string]any)
	if reason := onlyKeys(deploy, "replicas", "resources", "restart_policy", "labels"); reason != "" {
		return "deploy: " + reason
	}
	resources, _ := deploy["resources"].(map[string]any)
	reservations, _ := resources["reservations"].(map[string]any)
	if devices, _ := reservations["devices"].([]any); len(devices) > 0 {
		return "deploy: devices of the server are not allowed"
	}
	options, _ := service["security_opt"].([]any)
	for _, item := range options {
		if option, _ := item.(string); option != "no-new-privileges" && option != "no-new-privileges:true" {
			return "security_opt " + option + " is not allowed"
		}
	}
	sysctls, _ := service["sysctls"].(map[string]any)
	for _, key := range sortedKeys(sysctls) {
		if !strings.HasPrefix(key, "net.") {
			return "sysctl " + key + " is not allowed"
		}
	}
	return ""
}

// checkBuild keeps a build in the release's source: it may not read other
// folders of the server, nor get more powers than a container.
func (r appRoom) checkBuild(build map[string]any) string {
	for _, key := range sortedKeys(build) {
		if _, ok := buildKeys[key]; !ok && !strings.HasPrefix(key, "x-") {
			return key + " is not allowed"
		}
	}
	// Through links, as a mount: the build reads what they point at.
	context, _ := build["context"].(string)
	if !r.inSource(resolved(context)) {
		return "the context " + context + " is not in the app's source"
	}
	if dockerfile, _ := build["dockerfile"].(string); dockerfile != "" {
		if !filepath.IsAbs(dockerfile) {
			dockerfile = filepath.Join(context, dockerfile)
		}
		if !within(r.releasesDir, resolved(dockerfile)) {
			return "the Dockerfile " + dockerfile + " is not in the app's release"
		}
	}
	return ""
}

// checkMount allows the app's data folders, files of its source read-only,
// the folders the rules allow with mounts:, its named volumes and folders in
// memory. Data folders are never inside a folder a container can write.
func (r appRoom) checkMount(mount map[string]any) string {
	for _, key := range sortedKeys(mount) {
		if _, ok := mountKeys[key]; !ok {
			return "mount option " + key + " is not allowed"
		}
	}
	if mount["type"] == "volume" || mount["type"] == "tmpfs" {
		// compose lets a volume name only one of the file's own, which
		// check checks.
		return ""
	}
	if mount["type"] != "bind" {
		return fmt.Sprintf("a mount of type %v is not allowed", mount["type"])
	}
	if reason := onlyKeys(mount["bind"], "create_host_path", "selinux"); reason != "" {
		return "bind: " + reason
	}
	source, _ := mount["source"].(string)
	readOnly := mount["read_only"] == true
	if slices.Contains(timeFiles, source) && readOnly {
		return ""
	}
	// What the path points at now, through any link a container made.
	target, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "mounts " + source + ", which is not there"
	}
	if r.inSource(target) {
		// Code and config of the repo. Written, a container could leave a
		// link there for the next build or mount to follow.
		if !readOnly {
			return "mounts " + source + " of the source writable: mount it read-only, like ./config:/config:ro, and keep data in ./data"
		}
		return ""
	}
	if !r.inData(target) && !r.inMounts(target) {
		return "mounts " + source + ", which is not a folder of the app. Allow a folder of the server with mounts: <folder> in " + RulesFile()
	}
	for _, folder := range r.writable {
		if target != folder && within(folder, target) {
			return "mounts " + source + " inside " + folder + ", which a container can write"
		}
	}
	return ""
}

// inData reports whether path is in the app's data folders, never the
// folder of all of them.
func (r appRoom) inData(path string) bool {
	return path != r.dataDir && within(r.dataDir, path)
}

// inMounts reports whether path is in a folder of the server the rules
// allow with mounts:.
func (r appRoom) inMounts(path string) bool {
	return slices.ContainsFunc(r.mounts, func(folder string) bool { return within(folder, path) })
}

// inSource reports whether path is in the source of one of the app's
// releases, releases/<n>/src, and not in the rest of a release, which holds
// the compose file moin runs.
func (r appRoom) inSource(path string) bool {
	relative, err := filepath.Rel(r.releasesDir, path)
	if err != nil || !within(r.releasesDir, path) {
		return false
	}
	// <n>/src/...
	release, rest, _ := strings.Cut(relative, string(filepath.Separator))
	return release != "" && (rest == "src" || strings.HasPrefix(rest, "src"+string(filepath.Separator)))
}

// writableFolders are the folders the services of config mount writable,
// through their links.
func writableFolders(config map[string]any) []string {
	var folders []string
	services, _ := config["services"].(map[string]any)
	for _, item := range services {
		service, _ := item.(map[string]any)
		mounts, _ := service["volumes"].([]any)
		for _, item := range mounts {
			mount, _ := item.(map[string]any)
			if source, _ := mount["source"].(string); mount["type"] == "bind" && mount["read_only"] != true {
				folders = append(folders, resolved(source))
			}
		}
	}
	return folders
}

// onlyKeys names the first key of value that is not in keys, "" when
// there is none or value is not a map.
func onlyKeys(value any, keys ...string) string {
	settings, _ := value.(map[string]any)
	for _, key := range sortedKeys(settings) {
		if !slices.Contains(keys, key) && !strings.HasPrefix(key, "x-") {
			return key + " is not allowed"
		}
	}
	return ""
}

func sortedKeys(settings map[string]any) []string {
	return slices.Sorted(maps.Keys(settings))
}

// resolved is path through its links, such as a state folder that is a
// link to another disk, or path itself when it is not there yet. A mount
// uses filepath.EvalSymlinks instead: a mount that is not there is refused.
func resolved(path string) string {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		return target
	}
	return path
}

// readJSON reads a compose file moin wrote.
func readJSON(file string) (map[string]any, error) {
	body, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	return config, json.Unmarshal(body, &config)
}
