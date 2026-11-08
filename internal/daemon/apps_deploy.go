package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/protocol"
)

// deployStart runs a deploy the rules allow (DeployStart). The answer is
// deploy.done, never TypeError.
func (r *runner) deployStart(ctx context.Context, message protocol.Message, rules *protocol.Rules) {
	var start protocol.DeployStart
	if err := message.Decode(&start); err != nil {
		slog.Error("bad deploy.start", "error", err)
		return
	}
	if err := checkDeploy(rules, r.rulesFile, start); err != nil {
		slog.Warn("deploy refused", "app", start.App, "number", start.Number, "error", err)
		r.send(protocol.TypeDeployDone, "", protocol.DeployDone{DeployID: start.DeployID, Error: err.Error()})
		return
	}
	slog.Info("deploy", "app", start.App, "number", start.Number)
	if !r.beginWork() {
		r.send(protocol.TypeDeployDone, "", protocol.DeployDone{DeployID: start.DeployID, Error: errUpdating.Error()})
		return
	}
	defer r.endWork()
	r.apps.deploy(ctx, start)
}

// deploy runs one deploy and always ends with deploy.done.
func (a *apps) deploy(ctx context.Context, start protocol.DeployStart) {
	logf := func(format string, args ...any) {
		a.send(protocol.TypeDeployLog, "", protocol.DeployLog{DeployID: start.DeployID, Line: fmt.Sprintf(format, args...)})
	}
	done := protocol.DeployDone{DeployID: start.DeployID}
	detected, err := a.runDeploy(ctx, start, logf)
	done.Detected = detected
	a.mu.Lock()
	if before, ok := a.befores[start.DeployID]; ok {
		done.Before = &before
		delete(a.befores, start.DeployID)
	}
	a.mu.Unlock()
	if err != nil {
		done.Error = err.Error()
		logf("deploy failed: %s", err)
		a.markFailed(start.App, start.Number)
	} else {
		done.OK = true
		logf("v%d is live", start.Number)
	}
	a.send(protocol.TypeDeployDone, "", done)
}

// release is one deploy on its way to running. Every step of the deploy
// reads from it and writes down what it found.
type release struct {
	// start is what the hub asked for. A deploy without source takes the
	// image of the running release into start.Image.
	start protocol.DeployStart
	// dir is the folder of this release.
	dir string
	// composeFile is the compose.json moin runs for this release.
	composeFile string
	// config is the compose file as moin read it, before finish filled it in.
	config map[string]any
	// detected is what moin found out about the app so far.
	detected protocol.Detected
	// running names the images of the running release by service, for a
	// deploy without source.
	running map[string]string
	// logf writes a line to the deploy log.
	logf func(string, ...any)
}

// hasSource reports whether a deploy brings source code to build.
func hasSource(start protocol.DeployStart) bool {
	return start.SourceURL != "" || start.SourceNumber > 0
}

// runDeploy runs the steps of one deploy in order: make the release, take
// the compose file of an older release or fetch and build a new one, start
// it, then follow its databases and clean up.
func (a *apps) runDeploy(ctx context.Context, start protocol.DeployStart, logf func(string, ...any)) (protocol.Detected, error) {
	if !protocol.ValidAppName(start.App) || start.Number < 1 || start.AppID != "" && !appIDPattern.MatchString(start.AppID) {
		return protocol.Detected{}, errors.New("invalid app name, app ID or deploy number")
	}
	defer a.lock(start.App)()
	release, err := a.newRelease(start, logf)
	if err != nil {
		return protocol.Detected{}, err
	}
	if start.FromNumber > 0 {
		err = a.rollBack(ctx, release)
	} else {
		err = a.prepareRelease(ctx, release)
	}
	if err != nil {
		return release.detected, err
	}
	saveJSON(filepath.Join(release.dir, "detected.json"), release.detected)
	a.ownVolumes(ctx, release)
	err = a.startRelease(ctx, release)
	if a.backups != nil {
		// After a deploy, Litestream starts over: the next round follows.
		a.backups.letGo(release.start.App)
	}
	if err != nil {
		return release.detected, err
	}
	a.afterStart(ctx, release)
	return release.detected, nil
}

// newRelease keeps the app's ID and makes an empty folder for the release,
// with its kind for prune.
func (a *apps) newRelease(start protocol.DeployStart, logf func(string, ...any)) (*release, error) {
	if start.AppID != "" {
		err := os.MkdirAll(a.appDir(start.App), 0o700)
		if err == nil {
			err = os.WriteFile(filepath.Join(a.appDir(start.App), "app-id"), []byte(start.AppID), 0o600)
		}
		if err != nil {
			return nil, err
		}
	}
	dir := a.releaseDir(start.App, start.Number)
	_ = os.RemoveAll(dir)
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return nil, err
	}
	kind := releaseApply
	if hasSource(start) || start.Image != "" {
		kind = releaseBuild
	}
	err = os.WriteFile(filepath.Join(dir, "kind"), []byte(kind), 0o600)
	if err != nil {
		return nil, err
	}
	return &release{start: start, dir: dir, composeFile: filepath.Join(dir, "compose.json"), logf: logf}, nil
}

// rollBack copies the compose file of an older release into this one: the
// same images and the same env.
func (a *apps) rollBack(ctx context.Context, release *release) error {
	from := release.start.FromNumber
	release.logf("rolling back to v%d", from)
	old := a.releaseDir(release.start.App, from)
	body, err := os.ReadFile(filepath.Join(old, "compose.json"))
	if err != nil {
		return fmt.Errorf("v%d is no longer on the server", from)
	}
	detectedBody, _ := os.ReadFile(filepath.Join(old, "detected.json"))
	_ = json.Unmarshal(detectedBody, &release.detected)
	err = os.WriteFile(release.composeFile, body, 0o600)
	if err != nil {
		return err
	}
	base, err := os.ReadFile(filepath.Join(old, "base.json"))
	if err == nil {
		_ = os.WriteFile(filepath.Join(release.dir, "base.json"), base, 0o600)
	}
	return a.pinFile(ctx, release.start, release.composeFile)
}

// prepareRelease fetches the source, routes the app, writes compose.json and
// builds or pulls its images. A deploy without source takes the image of
// the running release into release.start.
func (a *apps) prepareRelease(ctx context.Context, release *release) error {
	// Asked before fetch: an image fetch takes from the running release is
	// already on the server and needs no pull.
	pull := release.start.Image != ""
	err := a.fetch(ctx, release)
	if err != nil {
		return err
	}
	saveJSON(filepath.Join(release.dir, "base.json"), release.config)
	release.detected.Image = release.start.Image
	err = a.writeCompose(ctx, release)
	if err != nil {
		return err
	}
	return a.build(ctx, release, pull)
}

// fetch fills in the release's compose file and findings, from its source
// or, for a deploy without source, from the running release.
func (a *apps) fetch(ctx context.Context, release *release) error {
	if hasSource(release.start) {
		return a.fetchSource(ctx, release)
	}
	return a.reuseRunningRelease(ctx, release)
}

// fetchSource downloads the source, or copies the one of an older release,
// and reads its compose file.
func (a *apps) fetchSource(ctx context.Context, release *release) error {
	start := release.start
	src := filepath.Join(release.dir, "src")
	var err error
	if start.SourceURL != "" {
		release.logf("downloading the source")
		err = download(ctx, start, src)
	} else {
		release.logf("building the source of v%d again", start.SourceNumber)
		err = a.copySource(ctx, start.App, start.SourceNumber, src)
	}
	if err != nil {
		return err
	}
	release.config, release.detected, err = a.resolve(ctx, start.App, src, start.Env)
	return err
}

// reuseRunningRelease keeps the compose file of the running release and its
// images. An image the deploy brings replaces the main one; without one the
// deploy takes the running image.
func (a *apps) reuseRunningRelease(ctx context.Context, release *release) error {
	current := a.current(release.start.App)
	if current == 0 {
		return errors.New("no release to take the compose file from: deploy the repo once with moin deploy")
	}
	err := loadBase(release, a.releaseDir(release.start.App, current))
	if err != nil {
		return err
	}
	if release.start.Image == "" {
		release.start.Image = release.detected.Image
	}
	return a.rebuildMissing(ctx, release)
}

// writeCompose routes the app, writes compose.json, pins its names and
// makes sure the proxy runs when the app has an address.
func (a *apps) writeCompose(ctx context.Context, release *release) error {
	detected, err := route(release.start.App, release.detected, release.start.Domains)
	release.detected = detected
	if err != nil {
		return err
	}
	err = finish(release)
	if err != nil {
		return err
	}
	err = a.pinFile(ctx, release.start, release.composeFile)
	if err != nil {
		return err
	}
	if !needsProxy(release.detected) {
		return nil
	}
	err = a.ensureProxy(ctx, release.logf)
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	return nil
}

// needsProxy reports whether the app has an address the proxy must serve.
func needsProxy(detected protocol.Detected) bool {
	return len(detected.Domains) > 0 || len(detected.ServiceDomains) > 0 || detected.Instant != ""
}

// build pulls the image built elsewhere, then builds the services that
// build here.
func (a *apps) build(ctx context.Context, release *release, pull bool) error {
	start := release.start
	detected := release.detected
	logf := release.logf
	if pull {
		logf("pulling %s", start.Image)
		err := a.pull(ctx, release)
		if err != nil {
			return fmt.Errorf("pull: %w", err)
		}
	} else if hasSource(start) {
		logf("detected %s, service %s on port %d", detected.Kind, detected.Service, detected.Port)
		if detected.Builder != "" {
			logf("→ Railpack: %s", detected.Builder)
		}
	} else {
		logf("applying env and domains to the running images")
	}
	if !builds(release.config) {
		return nil
	}
	logf("building")
	// A Railpack build reads the app's env as BuildKit secrets, from the
	// environment of compose, never from its arguments.
	var environment []string
	if detected.Kind == "railpack" {
		environment = buildEnv(start.Env)
	}
	err := a.run(ctx, logf, environment, "compose", "-p", a.projectOf(start.App), "-f", release.composeFile, "--progress", "plain", "build")
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	return nil
}

// startRelease starts the release and makes it the current one. When it
// does not come up, the release before it starts again.
func (a *apps) startRelease(ctx context.Context, release *release) error {
	start := release.start
	previous := a.current(start.App)
	if previous > 0 {
		a.copyDataBefore(ctx, release)
	}
	err := a.switchProject(ctx, start.App, nameOf(start), release.logf)
	if err != nil {
		return err
	}
	release.logf("starting")
	// A restart brings nothing new, so compose would leave the containers
	// as they are: recreate them.
	restart := start.SourceURL == "" && start.SourceNumber == 0 && start.Image == "" && start.FromNumber == 0
	err = a.start(ctx, start.App, release.composeFile, release.detected, restart, release.logf)
	if err != nil {
		return a.startPreviousAgain(ctx, release, previous, err)
	}
	return os.WriteFile(filepath.Join(a.appDir(start.App), "current"), []byte(strconv.Itoa(start.Number)), 0o600)
}

// copyDataBefore remembers the moment just before the new release touches
// the data, with a copy for Postgres: the hub names it "Before v12".
func (a *apps) copyDataBefore(ctx context.Context, release *release) {
	before := a.backups.copyBefore(ctx, release.start.App, "", release.logf)
	if before == nil {
		return
	}
	release.logf("noted the data before v%d", release.start.Number)
	a.mu.Lock()
	a.befores[release.start.DeployID] = *before
	a.mu.Unlock()
}

// startPreviousAgain starts the release that ran before, after the new one
// failed with startErr. It returns what the deploy failed with.
func (a *apps) startPreviousAgain(ctx context.Context, release *release, previous int, startErr error) error {
	start := release.start
	if previous <= 0 || previous == start.Number {
		return startErr
	}
	release.logf("starting v%d again", previous)
	previousFile := filepath.Join(a.releaseDir(start.App, previous), "compose.json")
	_ = a.pinFile(ctx, start, previousFile)
	rollbackErr := a.compose(ctx, start.App, previousFile, release.logf, "up", "-d", "--remove-orphans")
	if rollbackErr != nil {
		return startErr
	}
	return fmt.Errorf("%w; v%d is running again", startErr, previous)
}

// afterStart notes the databases the running release has, starts a round
// of backups for them, and drops old releases and images.
func (a *apps) afterStart(ctx context.Context, release *release) {
	databases, noBackup, err := a.scanDatabases(ctx, release.composeFile)
	if err == nil {
		release.detected.Databases = databases
		release.detected.NoBackup = noBackup
		writeErr := writeState(filepath.Join(release.dir, "detected.json"), release.detected)
		if writeErr != nil {
			release.logf("write down the databases: %s", writeErr)
		}
	}
	logDatabases(release.detected, release.logf)
	if a.backups != nil {
		go a.backups.tick(context.Background())
	}
	a.prune(ctx, release.start.App)
	a.cleanup(ctx)
}

// logDatabases tells the deploy log what moin backs up and what it cannot.
func logDatabases(detected protocol.Detected, logf func(string, ...any)) {
	for _, database := range detected.Databases {
		if database.Postgres() {
			logf("database %s (Postgres, service %s)", database.Path, database.Service)
			continue
		}
		logf("database %s (%s)", database.Path, database.HostPath)
	}
	for _, database := range detected.NoBackup {
		logf("database %s: %s", database.Path, noBackupText)
	}
}

// start brings the release up and checks that it stays up.
func (a *apps) start(ctx context.Context, app, composeFile string, detected protocol.Detected, recreate bool, logf func(string, ...any)) error {
	args := []string{"up", "-d", "--remove-orphans", "--wait", "--wait-timeout", strconv.Itoa(int(waitTimeout.Seconds()))}
	if recreate {
		args = append(args, "--force-recreate")
	}
	err := a.compose(ctx, app, composeFile, logf, args...)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	// A process that crashes right after starting passes --wait without a
	// healthcheck, and with its restart policy it may even be running again
	// a moment later. One that restarts once while its database starts is
	// fine: the service passes once it runs stableFor without a restart,
	// and fails if it does not within startWindow.
	running, err := a.stable(ctx, func() (bool, string) {
		return a.serviceState(ctx, app, composeFile, detected.Service)
	})
	if err != nil {
		return err
	}
	if !running {
		// The rollback replaces these containers, and their logs with them:
		// keep the crash in the deploy log.
		logf("last logs of %s:", detected.Service)
		_ = a.compose(ctx, app, composeFile, logf, "logs", "--no-color", "--tail", "20", detected.Service)
		return fmt.Errorf("service %s keeps stopping right after it starts; see the deploy log", detected.Service)
	}
	ids, _ := a.runningContainers(ctx, app, composeFile, detected.Service)
	containers := strings.Fields(ids)
	if len(containers) == 0 {
		return fmt.Errorf("service %s keeps stopping right after it starts; see the deploy log", detected.Service)
	}
	if detected.Config == nil || detected.Config.Health == "" {
		return nil
	}
	return a.checkHealth(ctx, containers[0], detected.Port, detected.Config.Health, logf)
}

// serviceState says whether a service runs, and a fingerprint of its
// restarts and containers for stable.
func (a *apps) serviceState(ctx context.Context, app, composeFile, service string) (bool, string) {
	ids, err := a.runningContainers(ctx, app, composeFile, service)
	ids = strings.TrimSpace(ids)
	running := err == nil && ids != ""
	return running, a.restartCount(ctx, app, composeFile, service) + " " + ids
}

// runningContainers lists the IDs of a service's running containers.
func (a *apps) runningContainers(ctx context.Context, app, composeFile, service string) (string, error) {
	return a.output(ctx, "docker", "compose", "-p", a.projectOf(app), "-f", composeFile, "ps", "--status", "running", "-q", service)
}

// restartCount is how often the first container of a service restarted,
// "" when it has none.
func (a *apps) restartCount(ctx context.Context, app, composeFile, service string) string {
	ids, _ := a.output(ctx, "docker", "compose", "-p", a.projectOf(app), "-f", composeFile, "ps", "-a", "-q", service)
	containers := strings.Fields(ids)
	if len(containers) == 0 {
		return ""
	}
	count, _ := a.output(ctx, "docker", "inspect", "-f", "{{.RestartCount}}", containers[0])
	return strings.TrimSpace(count)
}

// The window a started service gets to settle: it passes once it runs for
// stableFor with nothing about it changing, and fails after startWindow.
var (
	stableFor   = 10 * time.Second
	startWindow = 30 * time.Second
	stableTick  = time.Second
)

// stable polls state, which says whether the service runs and a fingerprint
// of its restarts, until it has run unchanged for stableFor. It reports
// false when startWindow ends first.
func (a *apps) stable(ctx context.Context, state func() (bool, string)) (bool, error) {
	deadline := time.Now().Add(startWindow)
	var since time.Time
	last := ""
	for {
		up, print := state()
		switch {
		case !up:
			since = time.Time{}
		case since.IsZero() || print != last:
			since = time.Now()
		case time.Since(since) >= stableFor:
			return true, nil
		}
		last = print
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(stableTick):
		}
	}
}

// checkHealth calls the health path on the container until it answers below 500.
func (a *apps) checkHealth(ctx context.Context, container string, port int, path string, logf func(string, ...any)) error {
	address, err := a.output(ctx, "docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", container)
	ips := strings.Fields(address)
	if err != nil || len(ips) == 0 {
		return errors.New("health check: the container has no IP address")
	}
	url := fmt.Sprintf("http://%s:%d/%s", ips[0], port, strings.TrimPrefix(path, "/"))
	logf("health check %s", url)
	return healthy(ctx, url, path)
}

// healthy calls url until it answers below 500, for up to healthTimeout.
func healthy(ctx context.Context, url, path string) error {
	// A redirect answers too: an app that sends http to its https domain,
	// like Ghost, runs; following it would leave the container's address.
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.Now().Add(healthTimeout)
	var last string
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode < 500 {
				return nil
			}
			last = response.Status
		} else {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("health check %s failed: %s", path, last)
}

// rebuildMissing lets finish build again the images of built services that
// are gone from the server, as after docker image prune, from the source
// their release kept. Without it compose would try to pull them. The main
// service is left alone when it runs another image.
func (a *apps) rebuildMissing(ctx context.Context, release *release) error {
	replaced := release.start.Image != ""
	services, _ := release.config["services"].(map[string]any)
	for name, item := range services {
		service, _ := item.(map[string]any)
		build, _ := service["build"].(map[string]any)
		image := release.running[name]
		if build == nil || image == "" || (replaced && name == release.detected.Service) {
			continue
		}
		_, err := a.output(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", image)
		if err == nil {
			continue
		}
		source, _ := build["context"].(string)
		_, err = os.Stat(source)
		if source == "" || err != nil {
			return fmt.Errorf("image %s of service %s is gone from the server, and so is the source to build it: deploy again with moin deploy", image, name)
		}
		release.logf("image %s of service %s is gone from the server: building it again from the kept source", image, name)
		delete(release.running, name)
	}
	return nil
}

// copySource copies the source a release kept into a new release.
func (a *apps) copySource(ctx context.Context, app string, number int, target string) error {
	source := filepath.Join(a.releaseDir(app, number), "src")
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("the source of v%d is no longer on the server: deploy again with moin deploy", number)
	}
	output, err := exec.CommandContext(ctx, "cp", "-a", source, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy the source of v%d: %s", number, strings.TrimSpace(string(output)))
	}
	return nil
}

// pull fetches the main service's image. A registry login goes into a
// throwaway Docker config, so no credentials stay on the server.
func (a *apps) pull(ctx context.Context, release *release) error {
	start := release.start
	args := []string{"compose", "-p", a.projectOf(start.App), "-f", release.composeFile, "--progress", "plain", "pull", release.detected.Service}
	if start.Registry == nil {
		return a.run(ctx, release.logf, nil, args...)
	}
	dir, err := os.MkdirTemp("", tempName("docker-"))
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	auth := base64.StdEncoding.EncodeToString([]byte(start.Registry.Username + ":" + start.Registry.Password))
	config, _ := json.Marshal(map[string]any{"auths": map[string]any{start.Registry.Host: map[string]string{"auth": auth}}})
	err = os.WriteFile(filepath.Join(dir, "config.json"), config, 0o600)
	if err != nil {
		return err
	}
	return a.run(ctx, release.logf, []string{"DOCKER_CONFIG=" + dir}, args...)
}

var imagePattern = regexp.MustCompile(`"image": "([^"]+)"`)

// prune keeps the newest releases and the images they use. The images of
// the releases it removes stay in the images file for cleanup.
func (a *apps) prune(ctx context.Context, app string) {
	a.rememberImages(app)
	entries, _ := os.ReadDir(filepath.Join(a.appDir(app), "releases"))
	current := a.current(app)
	releases := []protocol.Release{}
	for _, entry := range entries {
		number, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		kind, _ := os.ReadFile(filepath.Join(a.releaseDir(app, number), "kind"))
		if string(kind) == releaseFailed && number != current {
			_ = os.RemoveAll(a.releaseDir(app, number))
			continue
		}
		build := string(kind) == releaseBuild
		if len(kind) == 0 {
			// A release from before the kind file: one with a source was a
			// build. Keeping an old image deploy a little longer is harmless.
			_, err := os.Stat(filepath.Join(a.releaseDir(app, number), "src"))
			build = err == nil
		}
		releases = append(releases, protocol.Release{Number: number, Build: build})
	}
	kept := protocol.KeptReleases(releases, current)
	used := map[string]bool{}
	for _, release := range releases {
		if !kept[release.Number] {
			_ = os.RemoveAll(a.releaseDir(app, release.Number))
		}
	}
	for number := range kept {
		body, _ := os.ReadFile(filepath.Join(a.releaseDir(app, number), "compose.json"))
		for _, image := range imagePattern.FindAllStringSubmatch(string(body), -1) {
			used[image[1]] = true
		}
	}
	images, err := a.output(ctx, "docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", "--filter", "reference="+project(app)+"-*")
	if err != nil {
		return
	}
	for _, image := range strings.Fields(images) {
		if !used[image] {
			_ = exec.CommandContext(ctx, "docker", "image", "rm", image).Run()
		}
	}
}

// The kind file of a release says what it brought: a build (source or
// image), an apply without a build (restart, domains, rollback) or a failed
// deploy. prune keeps releases by it.
const (
	releaseBuild  = "build"
	releaseApply  = "apply"
	releaseFailed = "failed"
)

// markFailed marks the release of a failed deploy, unless it is running.
func (a *apps) markFailed(app string, number int) {
	if !protocol.ValidAppName(app) || number < 1 {
		return
	}
	release := a.releaseDir(app, number)
	if _, err := os.Stat(release); err != nil || number == a.current(app) {
		return
	}
	_ = os.WriteFile(filepath.Join(release, "kind"), []byte(releaseFailed), 0o600)
}
