package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/catalog"
	"github.com/axadrn/moin/protocol"
)

// deployNote is the file "deploying" in an app's folder, there from the
// start of a deploy to its end. One that is there while nobody holds the
// app's lock is of a deploy whose command stopped, like one whose SSH
// connection broke.
type deployNote struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
}

func (a *apps) noteFile(app string) string { return filepath.Join(a.appDir(app), "deploying") }

// recoverDeploys ends the deploys a stopped command left running. The
// daemon runs it when it starts and every minute.
func (a *apps) recoverDeploys(ctx context.Context) {
	notes, _ := filepath.Glob(a.noteFile("*"))
	for _, file := range notes {
		app := filepath.Base(filepath.Dir(file))
		unlock, ok := tryFileLock(a.lockFile(app))
		if !ok {
			// The deploy still runs.
			continue
		}
		var note deployNote
		if body, err := os.ReadFile(file); err == nil && json.Unmarshal(body, &note) == nil {
			a.endDeploy(ctx, app, note)
		}
		os.Remove(file)
		unlock()
	}
}

// endDeploy ends a deploy whose command stopped. If it got as far as
// being the current version, it worked. If not, the current version
// starts again, as after any failed deploy.
func (a *apps) endDeploy(ctx context.Context, app string, note deployNote) {
	current := a.current(app)
	if current == note.Number {
		return
	}
	a.markFailed(app, note.Number)
	slog.Warn("a deploy stopped before it was live", "app", app, "number", note.Number)
	if current > 0 && a.compose(ctx, app, a.composeJSON(app), func(string, ...any) {}, "up", "-d", "--remove-orphans") == nil {
		slog.Info("the release before it is running again", "app", app, "number", current)
	}
}

// release is one deploy on its way to running. Every step of the deploy
// reads from it and writes down what it found.
type release struct {
	// start is what was asked for. A deploy without source takes the
	// image of the running release into start.Image.
	start protocol.DeployStart
	// server is the config of the server when the deploy started.
	server Config
	// dir is the folder of this release.
	dir string
	// composeJSON is the compose.json moin runs for this release.
	composeJSON string
	// config is the compose file as moin read it, before finish filled it in.
	config map[string]any
	// detected is what moin found out about the app so far.
	detected protocol.Detected
	// running names the images of the running release by service, for a
	// deploy without source.
	running map[string]string
	// envFile is the app's env file, which every release reads, and env
	// the values in it when the deploy started.
	envFile string
	env     map[string]string
	// logf writes a line to the deploy log.
	logf func(string, ...any)
}

// hasSource reports whether a deploy brings source code to build.
func hasSource(start protocol.DeployStart) bool {
	return start.SourceURL != "" || start.SourceFile != "" || start.SourceNumber > 0 || start.Catalog != ""
}

// runDeploy runs the steps of one deploy in order: make the release, take
// the compose file of an older release or fetch and build a new one, start
// it, then follow its databases and clean up.
func (a *apps) runDeploy(ctx context.Context, start protocol.DeployStart, logf func(string, ...any)) (protocol.Detected, error) {
	if !protocol.ValidAppName(start.App) || start.Number < 1 || start.AppID != "" && !appIDPattern.MatchString(start.AppID) {
		return protocol.Detected{}, errors.New("invalid app name, app ID or deploy number")
	}
	defer a.lock(start.App)()
	note := a.noteFile(start.App)
	if err := writeState(note, deployNote{ID: start.DeployID, Number: start.Number}); err != nil {
		return protocol.Detected{}, fmt.Errorf("write the deploy down: %w", err)
	}
	defer os.Remove(note)
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
	if err != nil {
		return release.detected, err
	}
	a.afterStart(ctx, release)
	return release.detected, nil
}

// newRelease keeps the app's ID and makes an empty folder for the release,
// with its kind for prune.
func (a *apps) newRelease(start protocol.DeployStart, logf func(string, ...any)) (*release, error) {
	server, err := LoadConfig()
	if err != nil {
		return nil, err
	}
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
	err = os.MkdirAll(dir, 0o700)
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
	err = a.ensureEnv(start.App)
	if err != nil {
		return nil, err
	}
	env, err := a.env(start.App)
	if err != nil {
		return nil, err
	}
	return &release{start: start, server: server, dir: dir, composeJSON: filepath.Join(dir, "compose.json"), envFile: a.envFile(start.App), env: env, logf: logf}, nil
}

// rollBack runs an older release again: its compose file and images, with
// the env, the domains and the password as they are now. An app has one
// password, so a rollback never brings an old one back.
func (a *apps) rollBack(ctx context.Context, release *release) error {
	from := release.start.FromNumber
	release.logf("rolling back to v%d", from)
	old := a.releaseDir(release.start.App, from)
	if !exists(filepath.Join(old, "compose.json")) {
		return fmt.Errorf("v%d is no longer on the server", from)
	}
	err := loadBase(release, old)
	if err != nil {
		return err
	}
	if release.start.Image == "" {
		release.start.Image = release.detected.Image
	}
	release.detected.Image = release.start.Image
	err = copyIfThere(filepath.Join(old, ".env"), filepath.Join(release.dir, ".env"))
	if err != nil {
		return err
	}
	saveJSON(filepath.Join(release.dir, "base.json"), release.config)
	// An old release may come from before a rule, or its files may have
	// changed since: writeCompose checks it as a new one.
	return a.writeCompose(ctx, release)
}

// prepareRelease fetches the source, routes the app, writes compose.json and
// builds or pulls its images. A deploy without source takes the image of
// the running release into release.start.
func (a *apps) prepareRelease(ctx context.Context, release *release) error {
	// Asked before fetch: an image fetch takes from the running release is
	// already on the server and needs no pull, and so is one the CLI sent
	// with --image.
	pull := release.start.Image != "" && !strings.HasPrefix(release.start.Image, project(release.start.App)+":")
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
	switch {
	case start.Catalog != "":
		release.logf("taking %s from the catalog", start.Catalog)
		err = catalogSource(start.Catalog, src)
		if err == nil {
			err = a.makeSecrets(start.App, start.Catalog)
		}
		// The release reads the env with the secrets just made.
		if err == nil {
			release.env, err = a.env(start.App)
		}
	case start.SourceURL != "":
		release.logf("downloading the source")
		err = download(ctx, start, src)
	case start.SourceFile != "":
		err = unpackFile(start.SourceFile, src)
	default:
		release.logf("building the source of v%d again", start.SourceNumber)
		err = a.copySource(ctx, start.App, start.SourceNumber, src)
		if err == nil {
			err = a.dropGenerated(start.App, start.SourceNumber, src)
		}
	}
	if err != nil {
		return err
	}
	// The source's .env fills ${VAR} of the compose file too, when the
	// containers start: it goes next to compose.json (envArgs).
	err = copyIfThere(filepath.Join(src, ".env"), filepath.Join(release.dir, ".env"))
	if err != nil {
		return err
	}
	release.config, release.detected, err = a.resolve(ctx, start.App, src)
	return err
}

// copyIfThere copies the file from to to, 0600, when from is there.
func copyIfThere(from, to string) error {
	body, err := os.ReadFile(from)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return writeWhole(to, body)
}

// catalogSource writes the compose file of the catalog app into src, from
// the catalog built into this daemon.
func catalogSource(app, src string) error {
	compose, ok := catalog.Compose(app)
	if !ok {
		return fmt.Errorf("the catalog of this daemon has no %s: update the daemon", app)
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(src, "compose.yml"), compose, 0o644)
}

// reuseRunningRelease keeps the compose file of the running release and its
// images. An image the deploy brings replaces the main one; without one the
// deploy takes the running image.
func (a *apps) reuseRunningRelease(ctx context.Context, release *release) error {
	current := a.current(release.start.App)
	if current == 0 {
		return errors.New("no release to take the compose file from: link the app's repo, then deploy the image again")
	}
	dir := a.releaseDir(release.start.App, current)
	err := loadBase(release, dir)
	if err != nil {
		return err
	}
	err = copyIfThere(filepath.Join(dir, ".env"), filepath.Join(release.dir, ".env"))
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
	err = a.pinFile(ctx, release.start, release.composeJSON)
	if err != nil {
		return err
	}
	// Before anything builds or starts: the file as it will run.
	err = a.checkCompose(release)
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
	// env file, never from docker's own environment or its arguments.
	err := a.run(ctx, logf, nil, a.composeArgs(start.App, release.composeJSON, "--progress", "plain", "build")...)
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
	restart := start.SourceURL == "" && start.SourceFile == "" && start.SourceNumber == 0 && start.Image == "" && start.FromNumber == 0
	err = a.start(ctx, start.App, release.composeJSON, release.detected, restart, release.logf)
	if err != nil {
		return a.startPreviousAgain(ctx, release, previous, err)
	}
	return os.WriteFile(filepath.Join(a.appDir(start.App), "current"), []byte(strconv.Itoa(start.Number)), 0o600)
}

// copyDataBefore makes a copy of each database just before the new release
// touches the data.
func (a *apps) copyDataBefore(ctx context.Context, release *release) {
	before := a.backups.copyBefore(ctx, release.start.App, "", release.logf)
	if before == nil {
		return
	}
	release.logf("copied the data before v%d: %s", release.start.Number, protocol.BeforeCopyID(*before))
}

// startPreviousAgain starts the release that ran before, after the new one
// failed with startErr. It returns what the deploy failed with.
func (a *apps) startPreviousAgain(ctx context.Context, release *release, previous int, startErr error) error {
	start := release.start
	// A release that did not come up stops all its containers, so none of
	// them keeps writing while the next deploy checks its mounts.
	stop := func() { _ = a.compose(ctx, start.App, release.composeJSON, release.logf, "stop") }
	if previous <= 0 || previous == start.Number {
		stop()
		return startErr
	}
	release.logf("starting v%d again", previous)
	previousFile := filepath.Join(a.releaseDir(start.App, previous), "compose.json")
	_ = a.pinFile(ctx, start, previousFile)
	rollbackErr := a.compose(ctx, start.App, previousFile, release.logf, "up", "-d", "--remove-orphans")
	if rollbackErr != nil {
		stop()
		return startErr
	}
	return fmt.Errorf("%w; v%d is running again", startErr, previous)
}

// afterStart notes the databases the running release has, which the
// daemon copies from its next minute on, and drops old releases and images.
func (a *apps) afterStart(ctx context.Context, release *release) {
	databases, noBackup, err := a.scanDatabases(ctx, release.composeJSON)
	if err == nil {
		release.detected.Databases = databases
		release.detected.NoBackup = noBackup
		writeErr := writeState(filepath.Join(release.dir, "detected.json"), release.detected)
		if writeErr != nil {
			release.logf("write down the databases: %s", writeErr)
		}
	}
	logDatabases(release.detected, release.logf)
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

// start brings the release up and checks that it stays up. --wait waits
// until every service runs, and one with a Docker healthcheck until Docker
// says it is healthy; one that is not fails the start.
func (a *apps) start(ctx context.Context, app, composeJSON string, detected protocol.Detected, recreate bool, logf func(string, ...any)) error {
	args := []string{"up", "-d", "--remove-orphans", "--wait", "--wait-timeout", strconv.Itoa(int(waitTimeout.Seconds()))}
	if recreate {
		args = append(args, "--force-recreate")
	}
	err := a.compose(ctx, app, composeJSON, logf, args...)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	// A process that crashes right after starting passes --wait without a
	// healthcheck, and with its restart policy it may even be running again
	// a moment later. One that restarts once while its database starts is
	// fine: the service passes once it runs stableFor without a restart,
	// and fails if it does not within startWindow.
	running, err := a.stable(ctx, func() (bool, string) {
		return a.serviceState(ctx, app, composeJSON, detected.Service)
	})
	if err != nil {
		return err
	}
	if !running {
		// The rollback replaces these containers, and their logs with them:
		// keep the crash in the deploy log.
		logf("last logs of %s:", detected.Service)
		_ = a.compose(ctx, app, composeJSON, logf, "logs", "--no-color", "--tail", "20", detected.Service)
		return fmt.Errorf("service %s keeps stopping right after it starts; see the deploy log", detected.Service)
	}
	return nil
}

// serviceState says whether a service runs, and a fingerprint of its
// restarts and containers for stable.
func (a *apps) serviceState(ctx context.Context, app, composeJSON, service string) (bool, string) {
	ids, err := a.runningContainers(ctx, app, composeJSON, service)
	ids = strings.TrimSpace(ids)
	running := err == nil && ids != ""
	return running, a.restartCount(ctx, app, composeJSON, service) + " " + ids
}

// runningContainers lists the IDs of a service's running containers.
func (a *apps) runningContainers(ctx context.Context, app, composeJSON, service string) (string, error) {
	return a.output(ctx, "docker", a.composeArgs(app, composeJSON, "ps", "--status", "running", "-q", service)...)
}

// restartCount is how often the first container of a service restarted,
// "" when it has none.
func (a *apps) restartCount(ctx context.Context, app, composeJSON, service string) string {
	ids, _ := a.output(ctx, "docker", a.composeArgs(app, composeJSON, "ps", "-a", "-q", service)...)
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

// dropGenerated takes the compose.yml moin wrote for a repo without one out
// of the source it copied from release number, so resolve writes it again
// and finds the repo as the first deploy did: how it builds, and for
// Railpack a new plan next to src, with the env names as they are now.
func (a *apps) dropGenerated(app string, number int, src string) error {
	var detected protocol.Detected
	body, _ := os.ReadFile(filepath.Join(a.releaseDir(app, number), "detected.json"))
	_ = json.Unmarshal(body, &detected)
	if detected.Kind == "" || detected.Kind == "compose" {
		return nil
	}
	err := os.Remove(filepath.Join(src, "compose.yml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
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

// pull fetches the main service's image, with the server's login for its
// registry when it keeps one. The login goes into a throwaway Docker
// config, so Docker keeps no copy of it.
func (a *apps) pull(ctx context.Context, release *release) error {
	start := release.start
	args := a.composeArgs(start.App, release.composeJSON, "--progress", "plain", "pull", release.detected.Service)
	host := protocol.RegistryHost(start.Image)
	login := a.registryLogin(host)
	if login == "" {
		return a.run(ctx, release.logf, nil, args...)
	}
	dir, err := os.MkdirTemp("", tempName("docker-"))
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	auth := base64.StdEncoding.EncodeToString([]byte(login))
	config, _ := json.Marshal(map[string]any{"auths": map[string]any{host: map[string]string{"auth": auth}}})
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
	// An image deploy, a restart or a rollback runs from the source of an
	// older release: its build, its env files and its mounts are there. That
	// release stays while a kept one names it.
	for _, number := range a.sourcesNamed(app, kept) {
		kept[number] = true
	}
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

// sourcesNamed are the releases whose source the compose files of the
// releases in numbers name, as releases/<n>/src.
func (a *apps) sourcesNamed(app string, numbers map[int]bool) []int {
	pattern := regexp.MustCompile(regexp.QuoteMeta(filepath.Join(a.appDir(app), "releases")+"/") + `([0-9]+)/src[/"]`)
	found := []int{}
	for number := range numbers {
		body, _ := os.ReadFile(filepath.Join(a.releaseDir(app, number), "compose.json"))
		for _, match := range pattern.FindAllStringSubmatch(string(body), -1) {
			source, _ := strconv.Atoi(match[1])
			found = append(found, source)
		}
	}
	return found
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
