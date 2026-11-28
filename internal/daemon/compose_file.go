package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/axadrn/moin/protocol"
)

// The compose file of an app is everything moin runs for it, written to
// run anywhere: the compose.json of a release, with the proxy as a service
// of its own and no path of this server. Save it, put a .env with the
// app's values next to it, and docker compose up -d runs the app, its
// domains and HTTPS. It holds no value of the env: those stay on the
// server, and a file that holds one is not made (secretIn). Each ${NAME} stays as written, but
// for the settings the deploy filled in (takePlaces): image, ports,
// expose, volumes, user, the env PORT and the paths of a build.
//
// On the way out of the server:
//
//   - the source of a release, releases/<n>/src, is ".", the folder of the
//     file, so ./config stays ./config;
//   - the data folders moin moved to the app's volumes folder are where
//     the repo's compose file put them, such as ./data;
//   - app.env, which the app's services read, is .env, the file docker
//     compose also fills ${NAME} from;
//   - the instant address stays with moin: it names this server;
//   - the proxy's network is the file's own, not external;
//   - a service moin built builds again from ".". A Railpack build keeps
//     the image moin built: its plan is not in the repo.
//
// Keys are in a stable order, so the same release gives the same file.

// proxyService names the proxy in the compose file, or proxyName when the
// app has a service of that name.
const proxyService = "proxy"

// Compose is the compose file of an app's live release, for `moin
// compose`.
type Compose struct {
	App     string `json:"app"`
	Release int    `json:"release"`
	Compose string `json:"compose"`
}

// ComposeHere makes the compose file of the app's live release.
func ComposeHere(dir, app string) (Compose, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Compose{}, err
	}
	release := a.current(app)
	file, err := a.composeFile(app, release)
	return Compose{App: app, Release: release, Compose: file}, err
}

// composeFile makes the compose file of release number of an app.
func (a *apps) composeFile(app string, number int) (string, error) {
	dir := a.releaseDir(app, number)
	config, err := readJSON(filepath.Join(dir, "compose.json"))
	if err != nil {
		return "", err
	}
	base, _ := readJSON(filepath.Join(dir, "base.json"))
	var detected protocol.Detected
	body, _ := os.ReadFile(filepath.Join(dir, "detected.json"))
	_ = json.Unmarshal(body, &detected)

	config["name"] = a.projectOf(app)
	services, _ := config["services"].(map[string]any)
	baseServices, _ := base["services"].(map[string]any)
	for name, item := range services {
		service, _ := item.(map[string]any)
		baseService, _ := baseServices[name].(map[string]any)
		buildAgain(app, name, service, baseService, detected.Kind == "railpack")
	}
	if detected.Kind == "railpack" {
		// The env names railpackSecrets handed its build, which is gone.
		delete(config, "secrets")
	}
	dropPassword(app, config)
	dropInstant(app, config)
	config = takeAlong(config, a.serverPaths(app)).(map[string]any)
	takeEnvFile(services)
	held, err := a.secretIn(app, config)
	if err != nil {
		return "", err
	}
	if held != "" {
		return "", fmt.Errorf("it holds %s, so it stays on the server: deploy from source once to see it", held)
	}
	err = addProxy(config)
	if err != nil {
		return "", err
	}
	tunnel := a.proxyMode != nil && a.proxyMode() == "tunnel"
	return writeComposeYAML(config, tunnel)
}

// minSecret is the shortest env value secretIn looks for: a shorter one,
// such as 1, true or a port, is in many a compose file by chance.
const minSecret = 6

// secretIn names the env value of the app the compose file config of the
// release in dir holds, "" for none. It looks
// everywhere but in the settings the deploy fills in on purpose
// (takePlaces) and in the proxy's labels, which name the domains. A
// deploy from before v1.0.0-beta.35 wrote the values into base.json, and
// a restart takes the compose file from there: such a file never leaves
// the server.
func (a *apps) secretIn(app string, config map[string]any) (string, error) {
	secrets := map[string]string{}
	env, err := a.env(app)
	if err != nil {
		return "", err
	}
	for key, value := range env {
		if len(value) >= minSecret {
			secrets[value] = "the value of " + key
			secrets[escape(value)] = "the value of " + key
		}
	}
	if len(secrets) == 0 {
		return "", nil
	}
	scan, err := copyConfig(config)
	if err != nil {
		return "", err
	}
	services, _ := scan["services"].(map[string]any)
	for _, item := range services {
		service, _ := item.(map[string]any)
		for _, key := range placeKeys {
			delete(service, key)
		}
		environment, _ := service["environment"].(map[string]any)
		delete(environment, "PORT")
		build, _ := service["build"].(map[string]any)
		delete(build, "context")
		delete(build, "dockerfile")
		labels, _ := service["labels"].(map[string]any)
		for key := range labels {
			if strings.HasPrefix(key, "traefik.") {
				delete(labels, key)
			}
		}
	}
	held := ""
	sorted := slices.Sorted(maps.Keys(secrets))
	walkStrings(scan, func(text string) {
		for _, secret := range sorted {
			if held == "" && strings.Contains(text, secret) {
				held = secrets[secret]
			}
		}
	})
	return held, nil
}

// walkStrings calls see with every string in value, keys too.
func walkStrings(value any, see func(string)) {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			see(key)
			walkStrings(item, see)
		}
	case []any:
		for _, item := range value {
			walkStrings(item, see)
		}
	case string:
		see(value)
	}
}

// buildAgain makes a service moin built build again where the file runs,
// from the build its release read, and keeps the image of every other one.
// A Railpack build keeps its image.
func buildAgain(app, name string, service, baseService map[string]any, railpack bool) {
	image, _ := service["image"].(string)
	built := image == "" || strings.HasPrefix(image, project(app)+"-"+name+":")
	build, _ := service["build"].(map[string]any)
	if build == nil {
		build, _ = baseService["build"].(map[string]any)
	}
	if !built || build == nil || railpack {
		delete(service, "build")
		return
	}
	delete(service, "image")
	service["build"] = build
	// A hash of the env values, so a Railpack build sees a new one: it
	// builds the same without.
	args, _ := build["args"].(map[string]any)
	delete(args, "secrets-hash")
	if len(args) == 0 {
		delete(build, "args")
	}
}

// dropInstant takes the routers of the instant address out of the compose
// file: it names this server's IP, so it stays with moin. A service the
// proxy then routes nothing to leaves its network, and without any the
// file has no proxy.
func dropInstant(app string, config map[string]any) {
	instant := "traefik.http.routers." + project(app) + "-instant."
	routed := false
	services, _ := config["services"].(map[string]any)
	for _, item := range services {
		service, _ := item.(map[string]any)
		labels, _ := service["labels"].(map[string]any)
		hasRouter := false
		for key := range labels {
			if strings.HasPrefix(key, instant) {
				delete(labels, key)
			} else if strings.HasPrefix(key, "traefik.http.routers.") {
				hasRouter = true
			}
		}
		if hasRouter {
			routed = true
			continue
		}
		for key := range labels {
			if strings.HasPrefix(key, "traefik.") {
				delete(labels, key)
			}
		}
		networks, _ := service["networks"].(map[string]any)
		delete(networks, proxyNetwork)
	}
	if !routed {
		networks, _ := config["networks"].(map[string]any)
		delete(networks, proxyNetwork)
	}
}

// serverPaths are the paths of an app on this server, and how the compose
// file writes them.
type serverPaths struct {
	source  *regexp.Regexp
	volumes string
	envFile string
}

func (a *apps) serverPaths(app string) serverPaths {
	releases := regexp.QuoteMeta(filepath.Join(a.appDir(app), "releases") + "/")
	return serverPaths{
		source:  regexp.MustCompile(`^` + releases + `[0-9]+/src(/|$)`),
		volumes: filepath.Join(a.appDir(app), "volumes"),
		envFile: a.envFile(app),
	}
}

// path writes one path of the server as the compose file reads it, and
// leaves every other value as it is.
func (paths serverPaths) path(value string) string {
	switch {
	case value == paths.envFile:
		return ".env"
	case paths.source.MatchString(value):
		return "." + strings.TrimSuffix(paths.source.ReplaceAllString(value, "/"), "/")
	case value == paths.volumes || strings.HasPrefix(value, paths.volumes+"/"):
		return "." + strings.TrimPrefix(value, paths.volumes)
	}
	return value
}

// takeAlong writes every path of the server in value as the compose file
// reads it, and every whole number JSON read as one, so YAML writes 8080,
// not 8080.0 or 1.073741824e+09.
func takeAlong(value any, paths serverPaths) any {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			value[key] = takeAlong(item, paths)
		}
	case []any:
		for index, item := range value {
			value[index] = takeAlong(item, paths)
		}
	case string:
		return paths.path(value)
	case float64:
		if value == math.Trunc(value) && math.Abs(value) < 1<<53 {
			return int64(value)
		}
	}
	return value
}

// takeEnvFile names each env file of a service once: the source's .env and
// app.env are both .env now.
func takeEnvFile(services map[string]any) {
	for _, item := range services {
		service, _ := item.(map[string]any)
		files, _ := service["env_file"].([]any)
		if files == nil {
			continue
		}
		seen := map[string]bool{}
		kept := []any{}
		for _, file := range files {
			path, _ := file.(string)
			if entry, ok := file.(map[string]any); ok {
				path, _ = entry["path"].(string)
			}
			if seen[filepath.Clean(path)] {
				continue
			}
			seen[filepath.Clean(path)] = true
			kept = append(kept, file)
		}
		service["env_file"] = kept
	}
}

// addProxy adds the proxy as moin runs it, on ports 80 and 443 with
// certificates from Let's Encrypt, when a service of the app is on its
// network.
func addProxy(config map[string]any) error {
	networks, _ := config["networks"].(map[string]any)
	network, _ := networks[proxyNetwork].(map[string]any)
	if network == nil {
		return nil
	}
	delete(network, "external")
	services, _ := config["services"].(map[string]any)
	name := proxyService
	if _, taken := services[name]; taken {
		name = proxyName
	}
	if _, taken := services[name]; taken {
		return fmt.Errorf("the app has services named %s and %s, so the proxy has no name: rename one", proxyService, proxyName)
	}
	volumes := []any{"/var/run/docker.sock:/var/run/docker.sock:ro", "./traefik:/data"}
	command := []any{}
	for _, flag := range proxyFlags("direct") {
		command = append(command, flag)
	}
	services[name] = map[string]any{
		"image":    proxyImage,
		"command":  command,
		"ports":    []any{"80:80", "443:443"},
		"volumes":  volumes,
		"networks": []any{proxyNetwork},
		"restart":  "unless-stopped",
	}
	return nil
}

// composeParts are the parts of a compose file, in the order people write
// them. Others follow by name.
var composeParts = []string{"name", "services", "networks", "volumes", "configs", "secrets"}

// writeComposeYAML writes a compose file as YAML. tunnel says that the
// server gets its traffic through a Cloudflare Tunnel.
func writeComposeYAML(config map[string]any, tunnel bool) (string, error) {
	var out bytes.Buffer
	if tunnel {
		out.WriteString("# On the server, a Cloudflare Tunnel brings the traffic. Here the proxy takes ports 80 and 443.\n")
	}
	keys := []string{}
	for key := range config {
		if !slices.Contains(composeParts, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range append(slices.Clone(composeParts), keys...) {
		value, ok := config[key]
		if !ok {
			continue
		}
		encoder := yaml.NewEncoder(&out)
		encoder.SetIndent(2)
		err := encoder.Encode(map[string]any{key: value})
		if err != nil {
			return "", err
		}
		err = encoder.Close()
		if err != nil {
			return "", err
		}
	}
	return out.String(), nil
}
