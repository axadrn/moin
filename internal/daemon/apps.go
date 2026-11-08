package daemon

import (
	"context"
	"encoding/json"
	"errors"
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
//	releases/<n>/src           the source of deploy n
//	releases/<n>/compose.json  the resolved compose project moin runs, 0600
//	releases/<n>/kind          build, apply or failed, for prune
//	current                    the number of the running release
//	volumes/                   bind mounts that are not part of the repo
//
// compose.json is the user's compose.yml after `docker compose config`, with
// image tags per release, the env and the data mounts filled in. Starting an
// old compose.json again is a rollback: same images, same env.
const (
	maxSourceSize = 200 << 20
	waitTimeout   = 120 * time.Second
	// healthTimeout is as long as compose waits: a JVM app such as Metabase
	// takes a minute or more to answer on its first start.
	healthTimeout = 120 * time.Second
)

var appIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// apps runs the deploys, logs and removals the hub asks for.
type apps struct {
	dir  string
	send func(protocol.MessageType, string, any)
	// backups follows the databases a deploy adds or removes.
	backups *backups
	// proxyMode is "direct" or "tunnel", from the server config.
	proxyMode func() string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
	// befores is the moment just before each running deploy started its
	// new release over an older one, by deploy ID, for deploy.done.
	befores map[string]time.Time
}

func newApps(dir string, send func(protocol.MessageType, string, any)) *apps {
	return &apps{dir: filepath.Join(dir, "apps"), send: send, locks: map[string]*sync.Mutex{}, befores: map[string]time.Time{}}
}

// lock serializes everything that touches one app.
func (a *apps) lock(app string) func() {
	a.mu.Lock()
	lock := a.locks[app]
	if lock == nil {
		lock = &sync.Mutex{}
		a.locks[app] = lock
	}
	a.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (a *apps) appDir(app string) string { return filepath.Join(a.dir, app) }

func (a *apps) releaseDir(app string, number int) string {
	return filepath.Join(a.appDir(app), "releases", strconv.Itoa(number))
}

// appID is the app's ID in the hub, which every deploy brings,
// or "" before the first deploy that brought one.
func (a *apps) appID(app string) string {
	body, _ := os.ReadFile(filepath.Join(a.appDir(app), "app-id"))
	id := strings.TrimSpace(string(body))
	if !appIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// keepIDs stores the IDs the hub sends for apps that have none
// yet, the ones deployed before deploys brought it. A stored ID stays. It
// reports whether it stored one.
func (a *apps) keepIDs(ids map[string]string) (kept bool) {
	for app, id := range ids {
		if !protocol.ValidAppName(app) || !appIDPattern.MatchString(id) || a.appID(app) != "" {
			continue
		}
		if _, err := os.Stat(a.appDir(app)); err != nil {
			continue
		}
		if os.WriteFile(filepath.Join(a.appDir(app), "app-id"), []byte(id), 0o600) == nil {
			kept = true
		}
	}
	return kept
}

func (a *apps) current(app string) int {
	body, _ := os.ReadFile(filepath.Join(a.appDir(app), "current"))
	number, _ := strconv.Atoi(strings.TrimSpace(string(body)))
	return number
}

// sendLogs answers a logs request (LogsRequest) unless the rules say
// logs: off.
func (r *runner) sendLogs(ctx context.Context, message protocol.Message, rules *protocol.Rules) {
	var request protocol.LogsRequest
	if err := message.Decode(&request); err != nil {
		r.fail(message, err)
		return
	}
	if rules != nil && rules.LogsOff {
		r.fail(message, refused(r.rulesFile, "logs: off"))
		return
	}
	result, err := r.apps.logs(ctx, request)
	if err != nil {
		r.fail(message, err)
		return
	}
	r.send(protocol.TypeLogsResult, message.RequestID, result)
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

// removeApp answers app.remove (AppRemove).
func (r *runner) removeApp(ctx context.Context, message protocol.Message) {
	var request protocol.AppRemove
	if err := message.Decode(&request); err != nil {
		r.fail(message, err)
		return
	}
	if err := r.apps.remove(ctx, request.App); err != nil {
		r.fail(message, err)
		return
	}
	r.send(protocol.TypeOK, message.RequestID, nil)
}

// remove stops the app and deletes its containers, images and releases. The
// volumes stay, so the data survives. Its backups stop; what is in the
// bucket stays there.
func (a *apps) remove(ctx context.Context, app string) error {
	if !protocol.ValidAppName(app) {
		return errors.New("invalid app name")
	}
	defer a.lock(app)()
	args := []string{"compose", "-p", a.projectOf(app)}
	if current := a.current(app); current > 0 {
		args = append(args, "-f", filepath.Join(a.releaseDir(app, current), "compose.json"))
	}
	_, err := a.output(ctx, "docker", append(args, "down", "--rmi", "all", "--remove-orphans")...)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(a.appDir(app), "releases"))
	_ = os.Remove(filepath.Join(a.appDir(app), "current"))
	_ = os.Remove(filepath.Join(a.appDir(app), "app-id"))
	// Its containers are gone; volumes.json stays with the volumes.
	_ = os.Remove(filepath.Join(a.appDir(app), "project"))
	if a.backups != nil {
		a.backups.forget(a.backups.running())
	}
	return nil
}

// stopApp answers app.stop (AppStop).
func (r *runner) stopApp(ctx context.Context, message protocol.Message) {
	var request protocol.AppStop
	if err := message.Decode(&request); err != nil {
		r.fail(message, err)
		return
	}
	if err := r.apps.stop(ctx, request.App); err != nil {
		r.fail(message, err)
		return
	}
	r.send(protocol.TypeOK, message.RequestID, nil)
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
	_, err := a.output(ctx, "docker", "compose", "-p", a.projectOf(app), "-f", filepath.Join(a.releaseDir(app, current), "compose.json"), "stop")
	return err
}

// composeFile is the compose file of the app's live deploy.
func (a *apps) composeFile(app string) string {
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
