package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/protocol"
)

// Every app lives in <state dir>/apps/<app>:
//
//	app.env                    the app's env, the values as they are now, 0600
//	releases/<n>/src           the source of deploy n
//	releases/<n>/compose.json  the compose project moin runs, 0600
//	releases/<n>/.env          a copy of the source's .env, when it has one
//	releases/<n>/htpasswd      the app's password, when it has one, 0600
//	releases/<n>/kind          build, apply or failed, for prune
//	current                    the number of the running release
//	volumes/                   bind mounts that are not part of the repo
//
// compose.json is the user's compose.yml after `docker compose config`, with
// each ${VAR} kept, image tags per release and the data mounts filled in;
// the services that get the env read app.env. Every compose command on it
// reads .env and app.env (composeArgs), so Docker fills in ${VAR} when it
// starts the containers, and no file but app.env holds a value of the env.
// Starting an old compose.json again is a rollback: same images, the env
// as it is now.
const (
	maxSourceSize = 200 << 20
	// waitTimeout is how long compose waits for the services to run and be
	// healthy: a JVM app with a healthcheck takes a minute or more on its
	// first start.
	waitTimeout = 120 * time.Second
)

var appIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// apps runs the deploys, logs and removals of the apps on this server.
type apps struct {
	dir string
	// backups copies the databases before a deploy changes them.
	backups *backups
	// proxyMode is "direct" or "tunnel", from the server config.
	proxyMode func() string
	// registryLogin is the server's login for a registry host, as
	// <username>:<password>, "" for none.
	registryLogin func(host string) string
	// envMu keeps the changes of env files in a row.
	envMu sync.Mutex

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newApps(dir string) *apps {
	a := &apps{dir: filepath.Join(dir, "apps"), locks: map[string]*sync.Mutex{}}
	a.backups = newBackups(a)
	// The tunnel and the registry logins are in server.json, read each
	// time, so a change by another command counts at once.
	a.proxyMode = func() string { return newSecurity(dir, a).proxyMode() }
	a.registryLogin = func(host string) string { return newSecurity(dir, a).registryLogin(host) }
	return a
}

func (a *apps) lockFile(app string) string {
	return filepath.Join(filepath.Dir(a.dir), "locks", app+".lock")
}

// lock serializes everything that touches one app: in this process, and
// with a file lock across the daemon and every command over SSH.
func (a *apps) lock(app string) func() {
	a.mu.Lock()
	lock := a.locks[app]
	if lock == nil {
		lock = &sync.Mutex{}
		a.locks[app] = lock
	}
	a.mu.Unlock()
	lock.Lock()
	unlockFile, err := fileLock(a.lockFile(app))
	if err != nil {
		slog.Warn("lock the app across commands", "app", app, "error", err)
		return lock.Unlock
	}
	return func() {
		unlockFile()
		lock.Unlock()
	}
}

func (a *apps) appDir(app string) string { return filepath.Join(a.dir, app) }

// tempDir is the daemon's own folder for copies and checks, which no app
// mounts: an app name never starts with a dot.
func (a *apps) tempDir() string { return filepath.Join(a.dir, ".tmp") }

func (a *apps) releaseDir(app string, number int) string {
	return filepath.Join(a.appDir(app), "releases", strconv.Itoa(number))
}

func (a *apps) current(app string) int {
	body, _ := os.ReadFile(filepath.Join(a.appDir(app), "current"))
	number, _ := strconv.Atoi(strings.TrimSpace(string(body)))
	return number
}

// logs returns the last lines of every container of the app.
func (a *apps) logs(ctx context.Context, request protocol.LogsRequest) (protocol.LogsResult, error) {
	if !protocol.ValidAppName(request.App) {
		return protocol.LogsResult{}, errors.New("invalid app name")
	}
	tail := min(max(request.Tail, 1), 1000)
	output, err := a.output(ctx, "docker", "compose", "-p", a.projectOf(request.App), "logs", "--no-color", "--timestamps", "--tail", strconv.Itoa(tail))
	if err != nil {
		return protocol.LogsResult{}, err
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	return protocol.LogsResult{Lines: lines}, nil
}

// remove stops the app and deletes its containers, images and releases. The
// volumes stay, so the data survives. Its backups stop; what is in the
// bucket stays there.
func (a *apps) remove(ctx context.Context, app string) error {
	if !protocol.ValidAppName(app) {
		return errors.New("invalid app name")
	}
	defer a.lock(app)()
	// By the project's name only: compose finds the containers, networks
	// and images by their labels and reads no compose file, so a ${VAR}
	// the env no longer has cannot stop the removal.
	_, err := a.output(ctx, "docker", "compose", "-p", a.projectOf(app), "down", "--rmi", "all", "--remove-orphans")
	if err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(a.appDir(app), "releases"))
	_ = os.Remove(filepath.Join(a.appDir(app), "current"))
	// Its containers are gone; volumes.json stays with the volumes.
	_ = os.Remove(filepath.Join(a.appDir(app), "project"))
	// Its env goes with it: nothing runs that reads it.
	_ = os.Remove(a.envFile(app))
	// And the branch it follows, with its deploy key: no push brings the
	// app back.
	_ = os.RemoveAll(followDir(filepath.Dir(a.dir), app))
	return nil
}

// stop stops the containers of the app's current release. Their restart
// policy is unless-stopped, so they stay stopped after a reboot.
func (a *apps) stop(ctx context.Context, app string) error {
	if !protocol.ValidAppName(app) {
		return errors.New("invalid app name")
	}
	defer a.lock(app)()
	current := a.current(app)
	if current == 0 {
		return errors.New("the app has no release on this server")
	}
	_, err := a.output(ctx, "docker", a.composeArgs(app, a.composeJSON(app), "stop")...)
	return err
}

// composeJSON is the compose.json moin runs for the app's live deploy.
func (a *apps) composeJSON(app string) string {
	return filepath.Join(a.releaseDir(app, a.current(app)), "compose.json")
}

// live is what moin found in an app's live deploy, and the file it is
// in. The file is "" before the first deploy.
func (a *apps) live(app string) (protocol.Detected, string) {
	current := a.current(app)
	if current == 0 {
		return protocol.Detected{}, ""
	}
	file := filepath.Join(a.releaseDir(app, current), "detected.json")
	var detected protocol.Detected
	body, _ := os.ReadFile(file)
	_ = json.Unmarshal(body, &detected)
	return detected, file
}

func saveJSON(path string, value any) {
	body, err := json.Marshal(value)
	if err == nil {
		_ = os.WriteFile(path, body, 0o600)
	}
}
