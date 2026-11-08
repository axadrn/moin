package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/axadrn/moin/protocol"
)

// resolve reads the compose file of a release's source through `docker
// compose config` and moves the data mounts out of the release. It reports
// what it found. The app's env vars fill ${VAR} in the file, as a .env
// next to it would, so a service like postgres gets its password.
func (a *apps) resolve(ctx context.Context, app, src string, env map[string]string) (map[string]any, protocol.Detected, error) {
	detected := protocol.Detected{Kind: "compose"}
	if composePath(src) == "" {
		var err error
		detected.Kind, detected.Builder, err = a.generateCompose(ctx, app, src, env)
		if err != nil {
			return nil, detected, err
		}
	}
	config, err := a.readComposeConfig(ctx, app, src, env)
	if err != nil {
		return nil, detected, err
	}
	services, _ := config["services"].(map[string]any)
	if len(services) == 0 {
		return nil, detected, errors.New("compose.yml has no services")
	}
	detected.Config, err = readConfigBlock(config)
	if err != nil {
		return nil, detected, err
	}
	detected.Service = mainService(services, detected.Config)
	if detected.Service == "" {
		return nil, detected, errors.New("cannot tell which service gets the traffic: set x-moin.service")
	}
	detected.Port = mainPort(src, services[detected.Service], detected.Config)
	err = a.keepDataOutsideRelease(app, src, services)
	if err != nil {
		return nil, detected, err
	}
	return config, detected, nil
}

// generateCompose writes the compose file of a repo that has none. It
// returns how the repo builds and, for Railpack, what it builds with.
func (a *apps) generateCompose(ctx context.Context, app, src string, env map[string]string) (string, string, error) {
	kind, err := generate(src, app)
	if err != nil {
		return kind, "", err
	}
	if kind != "railpack" {
		return kind, "", nil
	}
	builder, err := a.railpackPrepare(ctx, src, filepath.Dir(src), env)
	return kind, builder, err
}

// readComposeConfig has docker compose read the compose file of a source.
func (a *apps) readComposeConfig(ctx context.Context, app, src string, env map[string]string) (map[string]any, error) {
	vars := make([]string, 0, len(env))
	for key, value := range env {
		vars = append(vars, key+"="+value)
	}
	raw, err := a.outputEnv(ctx, vars, "docker", "compose", "-p", project(app), "--project-directory", src, "-f", composePath(src), "config", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("compose.yml: %w", err)
	}
	var config map[string]any
	err = json.Unmarshal([]byte(raw), &config)
	if err != nil {
		return nil, fmt.Errorf("compose config: %w", err)
	}
	return config, nil
}

// readConfigBlock reads the x-moin block of a compose file, nil when there
// is none. What it could read comes back with an error too.
func readConfigBlock(config map[string]any) (*protocol.Config, error) {
	block, key, err := configBlock(config)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}
	settings := &protocol.Config{}
	body, _ := json.Marshal(block)
	err = json.Unmarshal(body, settings)
	if err != nil {
		return settings, fmt.Errorf("%s: %w", key, err)
	}
	if settings.Backups == nil {
		return settings, nil
	}
	_, err = settings.Backups.Schedule()
	if err != nil {
		return settings, fmt.Errorf("%s: %w", key, err)
	}
	return settings, nil
}

// keepDataOutsideRelease moves the data mounts of every service to the
// app's volumes folder, so the data outlives the release.
func (a *apps) keepDataOutsideRelease(app, src string, services map[string]any) error {
	volumesDir := filepath.Join(a.appDir(app), "volumes")
	for _, item := range services {
		service, _ := item.(map[string]any)
		mounts, _ := service["volumes"].([]any)
		for _, item := range mounts {
			mount, _ := item.(map[string]any)
			err := moveDataMount(mount, src, volumesDir)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// moveDataMount points one mount at the volumes folder when it holds data.
func moveDataMount(mount map[string]any, src, volumesDir string) error {
	source, _ := mount["source"].(string)
	if mount["type"] != "bind" || !within(src, source) {
		return nil
	}
	// A path the repo brings is code or config and stays in the
	// release. One it does not bring is data and must outlive it.
	_, err := os.Lstat(source)
	if err == nil {
		return nil
	}
	relative, _ := filepath.Rel(src, source)
	stable := filepath.Join(volumesDir, relative)
	err = os.MkdirAll(stable, 0o755)
	if err != nil {
		return err
	}
	mount["source"] = stable
	return nil
}

// ownVolumes hands the data directories moin made for an app to the user
// its service runs as. They are made by root, and an app that runs as
// another user could not write its database to them.
func (a *apps) ownVolumes(ctx context.Context, release *release) {
	if os.Geteuid() != 0 {
		return
	}
	var config composeConfig
	body, err := os.ReadFile(release.composeFile)
	if err != nil || json.Unmarshal(body, &config) != nil {
		return
	}
	app := release.start.App
	volumesDir := filepath.Join(a.appDir(app), "volumes")
	for name, service := range config.Services {
		folders := dataFolders(service, volumesDir)
		if len(folders) == 0 {
			continue
		}
		user := a.serviceUser(ctx, app, name, service)
		uid, gid, ok := numericUser(user)
		if !ok || uid == 0 {
			continue
		}
		for _, folder := range folders {
			handToUser(folder, user, uid, gid, release.logf)
		}
	}
}

// dataFolders are the folders of a service's mounts that moin made.
func dataFolders(service composeService, volumesDir string) []string {
	folders := []string{}
	for _, mount := range service.Volumes {
		if mount.Type == "bind" && within(volumesDir, mount.Source) {
			folders = append(folders, mount.Source)
		}
	}
	return folders
}

// serviceUser is the user a service runs as: the one compose.yml names, or
// else the one its image names.
func (a *apps) serviceUser(ctx context.Context, app, name string, service composeService) string {
	if service.User != "" {
		return service.User
	}
	image := service.Image
	if image == "" {
		image = project(app) + "-" + name
	}
	user, _ := a.output(ctx, "docker", "image", "inspect", "-f", "{{.Config.User}}", image)
	return strings.TrimSpace(user)
}

// handToUser gives a folder that root owns to the service's user.
func handToUser(folder, user string, uid, gid int, logf func(string, ...any)) {
	info, err := os.Stat(folder)
	if err != nil {
		return
	}
	owner, _ := info.Sys().(*syscall.Stat_t)
	if owner == nil || owner.Uid != 0 {
		return
	}
	err = os.Chown(folder, uid, gid)
	if err != nil {
		logf("cannot hand %s to user %s: %v", folder, user, err)
	}
}

// numericUser reads a compose or image user such as "1000", "1000:1000" or
// "65532:65532". A user name cannot be resolved outside the image.
func numericUser(user string) (int, int, bool) {
	name, group, found := strings.Cut(user, ":")
	uid, err := strconv.Atoi(name)
	if err != nil {
		return 0, 0, false
	}
	gid := uid
	if found {
		gid, err = strconv.Atoi(group)
		if err != nil {
			return 0, 0, false
		}
	}
	return uid, gid, true
}

// loadBase fills in a release from the release in dir: its resolved compose
// file, its findings and the image of every service.
func loadBase(release *release, dir string) error {
	var config map[string]any
	body, err := os.ReadFile(filepath.Join(dir, "base.json"))
	if err == nil {
		err = json.Unmarshal(body, &config)
	}
	if err != nil {
		return errors.New("the running release has no compose file to reuse: deploy the repo once with moin deploy")
	}
	var detected protocol.Detected
	body, _ = os.ReadFile(filepath.Join(dir, "detected.json"))
	_ = json.Unmarshal(body, &detected)
	detected.Databases = nil
	release.config = config
	release.detected = detected
	release.running = imagesOfRelease(dir)
	return nil
}

// imagesOfRelease names the image of every service of the release in dir.
func imagesOfRelease(dir string) map[string]string {
	var composed struct {
		Services map[string]struct {
			Image string `json:"image"`
		} `json:"services"`
	}
	body, _ := os.ReadFile(filepath.Join(dir, "compose.json"))
	_ = json.Unmarshal(body, &composed)
	images := map[string]string{}
	for name, service := range composed.Services {
		images[name] = service.Image
	}
	return images
}

// finish writes compose.json for a release: built services get an image tag
// per deploy, or keep their running image when there is no source; the main
// service runs start.Image when set; the app's services get the env; the
// main service gets the router labels; every service restarts unless
// stopped. release.config stays as it is.
func finish(release *release) error {
	config, err := copyConfig(release.config)
	if err != nil {
		return err
	}
	detected := release.detected
	env := release.start.Env
	services, _ := config["services"].(map[string]any)
	for name, item := range services {
		service, _ := item.(map[string]any)
		isMain := name == detected.Service
		// Asked before setImage, which removes the build.
		getsEnv := service["build"] != nil || isMain
		setImage(release, name, service)
		if getsEnv {
			addEnv(service, env)
		}
		if isMain && detected.Kind == "railpack" {
			railpackSecrets(config, service, env)
		}
		if service["restart"] == nil {
			service["restart"] = "unless-stopped"
		}
		routeService(release, config, name, service)
	}
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(release.composeFile, body, 0o600)
}

// copyConfig makes a copy of a compose file that shares nothing with it.
func copyConfig(base map[string]any) (map[string]any, error) {
	body, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	err = json.Unmarshal(body, &config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// setImage says which image a service runs in this release.
func setImage(release *release, name string, service map[string]any) {
	isMain := name == release.detected.Service
	isBuilt := service["build"] != nil
	switch {
	case isMain && release.start.Image != "":
		delete(service, "build")
		service["image"] = release.start.Image
	case isBuilt && release.running[name] != "":
		delete(service, "build")
		service["image"] = release.running[name]
	case isBuilt:
		service["image"] = fmt.Sprintf("%s-%s:%d", project(release.start.App), name, release.start.Number)
	}
}

// addEnv gives a service the app's env.
func addEnv(service map[string]any, env map[string]string) {
	environment, _ := service["environment"].(map[string]any)
	if environment == nil {
		environment = map[string]any{}
	}
	for key, value := range env {
		environment[key] = escape(value)
	}
	service["environment"] = environment
}

// routeService puts a service on the proxy when it has an address: the
// main service with the app's domains, another one with its own.
func routeService(release *release, config map[string]any, name string, service map[string]any) {
	app := release.start.App
	detected := release.detected
	domains := release.start.Domains
	if name == detected.Service && (len(detected.Domains) > 0 || detected.Instant != "") {
		addRouters(config, service, routes{
			app:            app,
			router:         project(app),
			domains:        detected.Domains,
			canonical:      detected.Canonical,
			port:           detected.Port,
			protect:        protectHash(domains),
			instant:        detected.Instant,
			instantProtect: instantProtectHash(domains),
		})
		return
	}
	serviceDomains := detected.ServiceDomains[name]
	if len(serviceDomains) == 0 {
		return
	}
	canonical := ""
	if slices.Contains(serviceDomains, detected.Canonical) {
		canonical = detected.Canonical
	}
	addRouters(config, service, routes{
		app:       app,
		router:    serviceRouter(app, name),
		domains:   serviceDomains,
		canonical: canonical,
		port:      servicePort(name, service, detected.Config),
		protect:   protectHash(domains),
	})
}

// escape keeps compose from interpolating a value.
func escape(value string) string { return strings.ReplaceAll(value, "$", "$$") }

// builds reports whether a compose file still has services to build.
func builds(config map[string]any) bool {
	services, _ := config["services"].(map[string]any)
	for _, item := range services {
		if service, _ := item.(map[string]any); service["build"] != nil {
			return true
		}
	}
	return false
}

func composePath(src string) string {
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		path := filepath.Join(src, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// generate writes what a repo without compose.yml needs: a compose.yml with
// one service and, for a Go module without Dockerfile, the Dockerfile too.
// A repo with none of these builds with Railpack, from a plan resolve has it
// write next to src.
func generate(src, app string) (string, error) {
	kind := "dockerfile"
	if _, err := os.Stat(filepath.Join(src, "Dockerfile")); err != nil {
		goMod, err := os.ReadFile(filepath.Join(src, "go.mod"))
		if err != nil {
			return "railpack", os.WriteFile(filepath.Join(src, "compose.yml"), []byte(railpackCompose(app)), 0o644)
		}
		kind = "go"
		err = os.WriteFile(filepath.Join(src, "Dockerfile"), []byte(goDockerfile(src, goMod)), 0o644)
		if err != nil {
			return "", err
		}
	}
	port := exposedPort(src)
	if port == 0 {
		port = 8080
	}
	compose := fmt.Sprintf(`services:
  web:
    build: .
    environment:
      PORT: "%d"
      DATA_DIR: /data
    volumes:
      - ./data:/data
`, port)
	return kind, os.WriteFile(filepath.Join(src, "compose.yml"), []byte(compose), 0o644)
}

var goVersionPattern = regexp.MustCompile(`(?m)^go (\d+\.\d+)`)

func goDockerfile(src string, goMod []byte) string {
	version := "1"
	if match := goVersionPattern.FindSubmatch(goMod); match != nil {
		version = string(match[1])
	}
	return fmt.Sprintf(`# Written by moin. Commit your own Dockerfile to change it.
FROM golang:%s-bookworm AS build
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY . .
RUN go build -o /app %s

FROM debian:bookworm-slim
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /app /app
ENV PORT=8080
EXPOSE 8080
CMD ["/app"]
`, version, mainPackage(src))
}

// mainPackage finds the package to build: the module root, cmd/server, or
// the only directory under cmd.
func mainPackage(src string) string {
	if isMain(src) {
		return "."
	}
	if isMain(filepath.Join(src, "cmd", "server")) {
		return "./cmd/server"
	}
	entries, _ := os.ReadDir(filepath.Join(src, "cmd"))
	mains := []string{}
	for _, entry := range entries {
		if entry.IsDir() && isMain(filepath.Join(src, "cmd", entry.Name())) {
			mains = append(mains, "./cmd/"+entry.Name())
		}
	}
	if len(mains) == 1 {
		return mains[0]
	}
	return "."
}

var packageMainPattern = regexp.MustCompile(`(?m)^package main\b`)

func isMain(dir string) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err == nil && packageMainPattern.Match(body) {
			return true
		}
	}
	return false
}

var exposePattern = regexp.MustCompile(`(?mi)^\s*EXPOSE\s+(\d+)`)

func exposedPort(src string) int {
	body, err := os.ReadFile(filepath.Join(src, "Dockerfile"))
	if err != nil {
		return 0
	}
	match := exposePattern.FindSubmatch(body)
	if match == nil {
		return 0
	}
	port, _ := strconv.Atoi(string(match[1]))
	return port
}

// mainService is the service that gets the traffic: the one x-moin names, the
// only one, or the only one moin builds.
func mainService(services map[string]any, config *protocol.Config) string {
	if config != nil && config.Service != "" {
		if _, ok := services[config.Service]; ok {
			return config.Service
		}
		return ""
	}
	if len(services) == 1 {
		for name := range services {
			return name
		}
	}
	built := []string{}
	for name, service := range services {
		if fields, _ := service.(map[string]any); fields["build"] != nil {
			built = append(built, name)
		}
	}
	if len(built) == 1 {
		return built[0]
	}
	if _, ok := services["web"]; ok {
		return "web"
	}
	return ""
}

// servicePort is the port a service other than the main one listens on:
// the one x-moin.ports names, else what its compose entry says, else 8080.
func servicePort(name string, service map[string]any, config *protocol.Config) int {
	if config != nil && config.Ports[name] > 0 {
		return config.Ports[name]
	}
	return mainPort("", service, nil)
}

// mainPort is the port the service listens on inside its container.
func mainPort(src string, service any, config *protocol.Config) int {
	if config != nil && config.Port > 0 {
		return config.Port
	}
	fields, _ := service.(map[string]any)
	if ports, _ := fields["ports"].([]any); len(ports) > 0 {
		if port, _ := ports[0].(map[string]any); port != nil {
			if target, ok := port["target"].(float64); ok {
				return int(target)
			}
		}
	}
	if expose, _ := fields["expose"].([]any); len(expose) > 0 {
		port, _ := strconv.Atoi(strings.Split(fmt.Sprint(expose[0]), "/")[0])
		if port > 0 {
			return port
		}
	}
	if environment, _ := fields["environment"].(map[string]any); environment != nil {
		if port, err := strconv.Atoi(fmt.Sprint(environment["PORT"])); err == nil && port > 0 {
			return port
		}
	}
	if port := exposedPort(src); src != "" && port > 0 {
		return port
	}
	return 8080
}

// composeConfig is the part of a release's compose.json that says where
// databases can be.
type composeConfig struct {
	Services map[string]composeService `json:"services"`
	Volumes  map[string]struct {
		Name string `json:"name"`
	} `json:"volumes"`
}

type composeService struct {
	Image   string         `json:"image"`
	User    string         `json:"user"`
	Volumes []composeMount `json:"volumes"`
}

type composeMount struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// configBlock is the daemon's block of compose.yml, x-moin. The block of
// the name the product had before is an error, never quietly ignored: it
// may hold the domains.
func configBlock(config map[string]any) (any, string, error) {
	key := "x-" + ourName
	if _, ok := config[protocol.OldConfigKey]; ok {
		return nil, protocol.OldConfigKey, fmt.Errorf("compose.yml has an %s block: rename it to %s", protocol.OldConfigKey, key)
	}
	return config[key], key, nil
}
