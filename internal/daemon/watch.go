package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
	"github.com/axadrn/moin/protocol"
)

// Every minute the daemon looks at its server, tells the watchers what it
// sees (a report) and alerts what is wrong: a backup that fails, a copy
// that did not restore, an app that keeps crashing, a disk that is almost
// full, and watchers it cannot reach. Each problem alerts once when it
// starts and once when it is over.

const (
	watchEvery = time.Minute
	// An app keeps crashing when its containers restarted crashRestarts
	// times within the last crashWindow looks.
	crashWindow   = 10
	crashRestarts = 3
	// The disk is almost full from diskFull percent on, and fine again
	// under diskFine.
	diskFull = 90
	diskFine = 85
	// watcherMisses is how many reports in a row no watcher took before
	// that is a problem.
	watcherMisses = 5
)

// watch is the daemon's look at its server, once a minute.
type watch struct {
	apps *apps
	// open are the problems that alerted and are not over, by their key,
	// with the alert. They are written down, so a restart alerts nothing
	// twice.
	open map[string]alert.Message
	// restarts are the restart counts of each app at the last looks.
	restarts map[string][]int
	// missed counts the reports in a row that no watcher took.
	missed int
}

// newWatch reads the problems an earlier daemon left open.
func newWatch(a *apps) *watch {
	w := &watch{apps: a, open: map[string]alert.Message{}, restarts: map[string][]int{}}
	body, _ := os.ReadFile(w.openFile())
	_ = json.Unmarshal(body, &w.open)
	return w
}

// openFile is where the open problems are written down.
func (w *watch) openFile() string {
	return filepath.Join(filepath.Dir(w.apps.dir), "alerts.json")
}

// loop looks at the server every minute until ctx ends.
func (w *watch) loop(ctx context.Context) {
	ticker := time.NewTicker(watchEvery)
	defer ticker.Stop()
	for {
		w.look(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// look is one round: end what stopped deploys left, report, alert.
func (w *watch) look(ctx context.Context) {
	config, err := LoadConfig()
	if err != nil {
		slog.Error("the config has a mistake: nothing is reported", "error", err)
		return
	}
	w.apps.recoverDeploys(ctx)
	containers := w.apps.containers(ctx)
	statuses := w.apps.backups.statuses(ctx)
	w.report(ctx, config, containers, statuses)
	w.alert(ctx, config, w.problems(config, containers, statuses))
}

// appContainers is what the containers of one app do now.
type appContainers struct {
	// running is false when one stopped that should run.
	running bool
	// restarts is how often Docker started them again, all together.
	restarts int
}

// containers looks at the containers of every app, by app.
func (a *apps) containers(ctx context.Context) map[string]appContainers {
	found := map[string]appContainers{}
	ids, err := a.output(ctx, "docker", "ps", "-aq", "--filter", "label=com.docker.compose.project")
	if err != nil || strings.TrimSpace(ids) == "" {
		return found
	}
	format := `{{index .Config.Labels "com.docker.compose.project"}} {{.State.Status}} {{.State.ExitCode}} {{.RestartCount}}`
	out, _ := a.output(ctx, "docker", append([]string{"inspect", "-f", format}, strings.Fields(ids)...)...)
	apps := map[string]string{}
	for _, app := range a.names() {
		apps[a.projectOf(app)] = app
	}
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) != 4 || apps[fields[0]] == "" {
			continue
		}
		app := apps[fields[0]]
		state, seen := found[app]
		if !seen {
			state.running = true
		}
		// A container that ended well, like a migration, is fine.
		endedWell := fields[1] == "exited" && fields[2] == "0"
		if fields[1] != "running" && !endedWell {
			state.running = false
		}
		restarts, _ := strconv.Atoi(fields[3])
		state.restarts += restarts
		found[app] = state
	}
	return found
}

// report tells every watcher how the server stands, and counts the
// rounds in which none of them took it.
func (w *watch) report(ctx context.Context, config Config, containers map[string]appContainers, statuses map[string][]protocol.DatabaseBackup) {
	if len(config.Watchers) == 0 {
		w.missed = 0
		return
	}
	r := report.Report{Server: config.Name, Version: build.Version, At: time.Now().UTC(), Every: watchEvery, Apps: []report.App{}}
	for _, app := range w.apps.names() {
		if w.apps.current(app) == 0 {
			continue
		}
		r.Apps = append(r.Apps, w.reportApp(app, containers[app], statuses[app]))
	}
	reached := false
	for _, watcher := range config.Watchers {
		err := report.Send(ctx, watcher.URL, watcher.Token, r)
		if err != nil {
			slog.Warn("a watcher did not take the report", "error", err)
			continue
		}
		reached = true
	}
	if reached {
		w.missed = 0
		return
	}
	w.missed++
}

// reportApp is one app in a report.
func (w *watch) reportApp(app string, containers appContainers, statuses []protocol.DatabaseBackup) report.App {
	detected, _ := w.apps.live(app)
	domains := slices.Clone(detected.Domains)
	if detected.Instant != "" {
		domains = append(domains, detected.Instant)
	}
	for _, service := range sortedServices(detected.ServiceDomains) {
		domains = append(domains, detected.ServiceDomains[service]...)
	}
	a := report.App{Name: app, Release: w.apps.current(app), Running: containers.running, Domains: domains, Databases: []report.Database{}}
	for _, status := range statuses {
		a.Databases = append(a.Databases, reportDatabase(status))
	}
	return a
}

// reportDatabase is one database in a report.
func reportDatabase(status protocol.DatabaseBackup) report.Database {
	d := report.Database{Name: status.Database, Kind: status.Kind, Error: status.Error}
	if d.Kind == "" {
		d.Kind = "sqlite"
	}
	if status.CopiedAt != nil {
		d.LastCopy = *status.CopiedAt
	}
	if status.CheckedAt != nil {
		d.LastCheck = *status.CheckedAt
		d.CheckOK = status.CheckError == ""
	}
	d.CheckedCopy, _ = protocol.CopyTime(status.CheckedCopy)
	if d.Error == "" {
		d.Error = status.CheckError
	}
	return d
}

// sortedServices are the keys of a map of lists, sorted.
func sortedServices(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// problems are what is wrong on the server now, by a key that stays the
// same while the problem lasts.
func (w *watch) problems(config Config, containers map[string]appContainers, statuses map[string][]protocol.DatabaseBackup) map[string]alert.Message {
	problems := map[string]alert.Message{}
	add := func(key, app, title, text string) {
		problems[key] = alert.Message{Server: config.Name, App: app, Title: title, Text: text}
	}
	for app, databases := range statuses {
		for _, status := range databases {
			// A server without a bucket is no news: the deploy said so.
			if status.Error != "" && config.Bucket != nil {
				add("backup/"+app+"/"+status.Database, app, fmt.Sprintf("%s: the backup of %s fails", app, status.Database), status.Error)
			}
			if status.CheckError != "" {
				add("check/"+app+"/"+status.Database, app, fmt.Sprintf("%s: a backup of %s did not restore", app, status.Database), status.CheckError)
			}
		}
	}
	for app, state := range containers {
		if w.crashing(app, state.restarts) {
			add("crash/"+app, app, app+" keeps crashing", fmt.Sprintf("Its containers restarted %d times. See why: %s logs %s --app %s", state.restarts, build.Name, config.Name, app))
		}
	}
	if percent := diskPercent(w.apps.dir); w.diskFull(percent) {
		add("disk", "", fmt.Sprintf("the disk is %d%% full", percent), "Backups and deploys stop when it is full.")
	}
	if w.missed >= watcherMisses {
		add("watchers", "", "no watcher is reachable", fmt.Sprintf("The last %d reports reached none of them. Nobody notices when this server goes away.", w.missed))
	}
	return problems
}

// crashing notes the restarts of app and says whether it keeps crashing.
// An app that did stays a problem until a whole window passed without a
// restart.
func (w *watch) crashing(app string, restarts int) bool {
	seen := append(w.restarts[app], restarts)
	if len(seen) > crashWindow {
		seen = seen[len(seen)-crashWindow:]
	}
	w.restarts[app] = seen
	// A new container counts from zero again.
	since := max(restarts-slices.Min(seen), 0)
	if _, open := w.open["crash/"+app]; open {
		return since > 0
	}
	return since >= crashRestarts
}

// diskFull says the disk is almost full, and stays so until it has clearly
// more room again.
func (w *watch) diskFull(percent int) bool {
	if _, open := w.open["disk"]; open {
		return percent >= diskFine
	}
	return percent >= diskFull
}

// diskPercent is how full the file system of dir is, 0 when unknown.
func diskPercent(dir string) int {
	var stat syscall.Statfs_t
	if syscall.Statfs(dir, &stat) != nil || stat.Blocks == 0 {
		return 0
	}
	return int(100 * (stat.Blocks - stat.Bavail) / stat.Blocks)
}

// alert sends what is new among the problems and what is over, once each.
// A problem no destination took alerts again in the next round.
func (w *watch) alert(ctx context.Context, config Config, problems map[string]alert.Message) {
	changed := false
	for key, message := range problems {
		if _, open := w.open[key]; open {
			continue
		}
		slog.Warn("alert", "title", message.Title, "text", message.Text)
		if sendAlert(ctx, config, message) {
			w.open[key] = message
			changed = true
		}
	}
	for key, message := range w.open {
		if _, still := problems[key]; still {
			continue
		}
		message.OK, message.Title, message.Text = true, "over: "+message.Title, ""
		slog.Info("alert", "title", message.Title)
		if sendAlert(ctx, config, message) {
			delete(w.open, key)
			changed = true
		}
	}
	if changed {
		if err := writeState(w.openFile(), w.open); err != nil {
			slog.Error("write down the open alerts", "error", err)
		}
	}
}

// sendAlert sends message to every destination and says whether one took
// it. Without destinations the log is the alert.
func sendAlert(ctx context.Context, config Config, message alert.Message) bool {
	sent := len(config.Alerts) == 0
	for _, destination := range config.Alerts {
		err := alert.Send(ctx, destination, message)
		if err != nil {
			slog.Error("an alert did not go out", "kind", destination.Kind, "error", err)
			continue
		}
		sent = true
	}
	return sent
}
