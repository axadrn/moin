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
// compose config` and moves the data mounts out of the release. It returns
// the compose file as moin keeps it and reports what it found. The app's
// env vars fill ${VAR} in the file, as a .env next to it would, so a
// service like postgres gets its password, but only when Docker starts the
// containers: the file moin keeps names them (readComposeConfig).
func (a *apps) resolve(ctx context.Context, app, src string) (map[string]any, protocol.Detected, error) {
	detected := protocol.Detected{Kind: "compose"}
	if composePath(src) == "" {
		env, err := a.env(app)
		if err != nil {
			return nil, detected, err
		}
		detected.Kind, detected.Builder, err = a.generateCompose(ctx, app, src, env)
		if err != nil {
			return nil, detected, err
		}
	}
	config, filled, err := a.readComposeConfig(ctx, app, src)
	if err != nil {
		return nil, detected, err
	}
	services, _ := filled["services"].(map[string]any)
	if len(services) == 0 {
		return nil, detected, errors.New("compose.yml has no services")
	}
	if _, ok := config[oldBlock]; ok {
		return nil, detected, errOldBlock
	}
	detected.Service = mainService(services)
	if detected.Service == "" {
		return nil, detected, errors.New("which service gets the traffic? Name it web")
	}
	detected.Port = servicePort(src, services[detected.Service])
	kept, _ := config["services"].(map[string]any)
	err = a.keepDataOutsideRelease(app, src, kept)
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

// readComposeConfig has docker compose read the compose file of a source
// twice, with the env files of envArgs:
//
//   - filled, with each ${VAR} filled in, is what will run. moin checks it
//     and finds the main service and its port in it, in memory only.
//   - config, as written, keeps each ${VAR}. It is the file moin edits and
//     keeps, so no value of the env ends up in it: Docker fills them in when
//     it starts the containers.
//
// The settings that say where a service runs are the exception: config
// takes them as filled (takePlaces).
//
// The env files a service names stay files it reads when it starts, so
// checkSourceFiles checks them in config: compose 2.38 drops them from the
// filled read, even with --no-env-resolution.
func (a *apps) readComposeConfig(ctx context.Context, app, src string) (config, filled map[string]any, err error) {
	err = refuseIncludes(composePath(src))
	if err != nil {
		return nil, nil, err
	}
	config, err = a.composeConfig(ctx, app, src, "--no-interpolate")
	if err != nil {
		return nil, nil, err
	}
	err = checkSourceFiles(src, config)
	if err != nil {
		return nil, nil, err
	}
	filled, err = a.composeConfig(ctx, app, src, "--no-env-resolution")
	if err != nil {
		return nil, nil, err
	}
	mapLists(config)
	takePlaces(config, filled)
	return config, filled, nil
}

// composeConfig runs docker compose config on the compose file of a source.
func (a *apps) composeConfig(ctx context.Context, app, src, mode string) (map[string]any, error) {
	args := []string{"compose", "-p", project(app), "--project-directory", src, "-f", composePath(src)}
	args = append(args, a.envArgs(app, src)...)
	raw, err := a.output(ctx, "docker", append(args, "config", "--format", "json", mode)...)
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

// placeKeys are the settings of a service that say where it runs: its
// image, its ports and its folders on the server. compose cannot read a
// mount with ${VAR} in it unfilled, moin checks these as they run and moves
// data mounts, and none of them holds a secret. They stay as the deploy
// filled them: a rollback runs the same images and folders.
var placeKeys = []string{"image", "ports", "expose", "volumes"}

// takePlaces copies the place settings of every service, and the folder
// and Dockerfile of its build, from filled into config.
func takePlaces(config, filled map[string]any) {
	services, _ := config["services"].(map[string]any)
	filledServices, _ := filled["services"].(map[string]any)
	for name, item := range services {
		service, _ := item.(map[string]any)
		from, _ := filledServices[name].(map[string]any)
		copyKeys(service, from, placeKeys...)
		build, _ := service["build"].(map[string]any)
		fromBuild, _ := from["build"].(map[string]any)
		if build != nil {
			copyKeys(build, fromBuild, "context", "dockerfile")
		}
	}
}

// copyKeys sets keys of to as they are in from, and removes those from
// lacks.
func copyKeys(to, from map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := from[key]; ok {
			to[key] = value
		} else {
			delete(to, key)
		}
	}
}

// mapLists writes as maps the settings moin edits or checks that the
// compose file wrote as lists: labels, environment and sysctls as
// KEY=value, networks as names. compose does it only when it fills the
// file in.
func mapLists(config map[string]any) {
	services, _ := config["services"].(map[string]any)
	for _, item := range services {
		service, _ := item.(map[string]any)
		for _, key := range []string{"labels", "environment", "sysctls", "networks"} {
			if list, ok := service[key].([]any); ok {
				service[key] = listToMap(list)
			}
		}
	}
}

// listToMap reads KEY=value items as a map, and an item without = as a key
// without a value, as compose does.
func listToMap(list []any) map[string]any {
	values := map[string]any{}
	for _, item := range list {
		key, value, found := strings.Cut(fmt.Sprint(item), "=")
		if found {
			values[key] = value
		} else {
			values[key] = nil
		}
	}
	return values
}

// oldBlock is the block of compose.yml moin read its settings from before.
// moin keeps every setting now, so a file that still has it fails the
// deploy: the block may hold domains the app would quietly lose.
var (
	oldBlock    = "x-" + ourName
	errOldBlock = fmt.Errorf("%s is gone: compose.yml is plain Docker now, and %s keeps its settings. Remove the %s block. https://%s.run/docs/guides/compose", oldBlock, ourName, oldBlock, ourName)
)

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
	// A container may have left a link in the data folders: make nothing
	// through it, as root, outside them.
	err = os.MkdirAll(volumesDir, 0o755)
	if err != nil {
		return err
	}
	parent := resolved(existingParent(stable))
	if parent != resolved(volumesDir) && !within(resolved(volumesDir), parent) {
		return fmt.Errorf("the data folder %s leads out of the app, so the server refuses it", relative)
	}
	err = os.MkdirAll(stable, 0o755)
	if err != nil {
		return err
	}
	mount["source"] = stable
	return nil
}

// existingParent is path, or the nearest folder above it that is there.
func existingParent(path string) string {
	for {
		if _, err := os.Lstat(path); err == nil || path == filepath.Dir(path) {
			return path
		}
		path = filepath.Dir(path)
	}
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
		return errors.New("the running release has no compose file to reuse: link the app's repo, then deploy the image again")
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
// service runs start.Image when set; the app's services read the env file;
// the main service gets the router labels, and the app's password goes in
// the release's htpasswd; every service restarts unless stopped.
// release.config stays as it is.
func finish(release *release) error {
	config, err := copyConfig(release.config)
	if err != nil {
		return err
	}
	if protect := protectHash(release.start.Domains); protect != "" {
		err = writeWhole(filepath.Join(release.dir, "htpasswd"), []byte(protect+"\n"))
		if err != nil {
			return err
		}
	}
	detected := release.detected
	env := release.env
	services, _ := config["services"].(map[string]any)
	for name, item := range services {
		service, _ := item.(map[string]any)
		isMain := name == detected.Service
		// Asked before setImage, which removes the build.
		getsEnv := service["build"] != nil || isMain
		setImage(release, name, service)
		if getsEnv {
			addEnv(service, release.envFile, env)
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

// addEnv has a service read the app's env file when it starts, after the
// env files the compose file names, so no value is in compose.json. A key
// the compose file sets too goes from its environment: the app's env wins,
// and compose would let environment win over env_file.
func addEnv(service map[string]any, file string, env map[string]string) {
	environment, _ := service["environment"].(map[string]any)
	for key := range env {
		delete(environment, key)
	}
	files, _ := service["env_file"].([]any)
	service["env_file"] = append(files, file)
}

// routeService puts a service on the proxy when it has an address: the
// main service with the app's domains, another one with its own.
func routeService(release *release, config map[string]any, name string, service map[string]any) {
	app := release.start.App
	detected := release.detected
	usersFile := ""
	if protectHash(release.start.Domains) != "" {
		usersFile = proxyUsersFile(app, release.start.Number)
	}
	if name == detected.Service && (len(detected.Domains) > 0 || detected.Instant != "") {
		addRouters(config, service, routes{
			app:       app,
			router:    project(app),
			domains:   detected.Domains,
			canonical: detected.Canonical,
			port:      detected.Port,
			usersFile: usersFile,
			instant:   detected.Instant,
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
		port:      servicePort("", service),
		usersFile: usersFile,
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

// mainService is the service that gets the traffic: the only one, else the
// only one moin builds, else the one named web.
func mainService(services map[string]any) string {
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

// servicePort is the port a service listens on inside its container: its
// ports target, else expose, else its env PORT, else EXPOSE of the
// Dockerfile in src, else 8080. The main service passes the source as src,
// the others none.
func servicePort(src string, service any) int {
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
	if src != "" {
		if port := exposedPort(src); port > 0 {
			return port
		}
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
