package daemon

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// Status is what runs on this server, for `moin` and `moin apps` on your
// laptop.
type Status struct {
	Version  string      `json:"version"`
	Apps     []AppStatus `json:"apps"`
	Watchers []string    `json:"watchers"`
	// Needs is what on this server needs you now.
	Needs []Need `json:"needs,omitempty"`
	// PublicSSH: port 22 is open to the internet.
	PublicSSH bool `json:"public_ssh"`
	// KnocksToday is how many connections the firewall dropped today.
	KnocksToday int64 `json:"knocks_today"`
}

// Need is one thing that needs you, and the command that settles it.
type Need struct {
	Text    string `json:"text"`
	Command string `json:"command"`
}

// AppStatus is one app: its live release, whether its containers run, and
// its addresses.
type AppStatus struct {
	Name      string   `json:"name"`
	Release   int      `json:"release"`
	Running   bool     `json:"running"`
	Addresses []string `json:"addresses"`
}

// StatusHere looks at every app on this server.
func StatusHere(ctx context.Context, dir string) Status {
	a := hereApps(dir)
	status := Status{Version: build.Version, Apps: []AppStatus{}, Watchers: []string{}}
	if watchers, err := WatchersHere(); err == nil && watchers != nil {
		status.Watchers = watchers
	}
	status.PublicSSH = PublicSSHHere(ctx, dir)
	status.KnocksToday = knocksToday(dir)
	if config, err := LoadConfig(); err == nil {
		statuses := a.backups.statuses(ctx)
		status.Needs = append(backupKeyNeeds(config, statuses), backupNeeds(config, statuses)...)
		status.Needs = append(status.Needs, tailnetStatusNeeds(ctx, dir, config.Name)...)
	}
	for _, app := range a.names() {
		release := a.current(app)
		if release == 0 {
			continue
		}
		detected, _ := a.live(app)
		status.Apps = append(status.Apps, AppStatus{
			Name:      app,
			Release:   release,
			Running:   a.running(ctx, app),
			Addresses: addresses(detected),
		})
	}
	return status
}

// backupKeyNeeds says what your backup key needs: a server with a bucket,
// or one whose copies a watcher took, is one fire away from copies nobody
// can read when only the server itself could open them.
func backupKeyNeeds(config Config, statuses map[string][]protocol.DatabaseBackup) []Need {
	copied := false
	for _, databases := range statuses {
		copied = copied || slices.ContainsFunc(databases, func(database protocol.DatabaseBackup) bool { return database.CopiedAt != nil })
	}
	switch {
	case config.Bucket == nil && !copied:
		return nil
	case config.BackupKey == nil:
		return []Need{{Text: config.Name + ": no backup key of yours. When the server is gone, nobody can read its copies.", Command: build.Name + " bucket key " + config.Name}}
	case !config.BackupKey.Confirmed:
		return []Need{{Text: config.Name + ": your backup key is not confirmed. Without it nobody can read the copies when the server is gone.", Command: build.Name + " bucket confirm " + config.Name}}
	}
	return nil
}

// backupNeeds says what the backups of the apps need: an app with a
// database on a server without a bucket has no copy at all, unless a
// watcher took one, and a copy no check has proven may not come back
// either. They come from what the daemon wrote down, by app.
func backupNeeds(config Config, statuses map[string][]protocol.DatabaseBackup) []Need {
	var needs []Need
	for _, app := range slices.Sorted(maps.Keys(statuses)) {
		databases := statuses[app]
		if len(databases) == 0 {
			continue
		}
		copied := slices.ContainsFunc(databases, func(database protocol.DatabaseBackup) bool { return database.CopiedAt != nil })
		if config.Bucket == nil && !copied {
			needs = append(needs, Need{Text: app + ": no backup yet.", Command: build.Name + " bucket set " + config.Name})
			continue
		}
		for _, database := range databases {
			if database.CheckError == "" {
				continue
			}
			what := "today's backup"
			if len(databases) > 1 {
				what += " of " + database.Database
			}
			proven := "Last proven copy: none yet."
			if at, ok := protocol.CopyTime(database.CheckedCopy); ok {
				proven = "Last proven copy: " + at.UTC().Format("Jan 2 15:04 UTC") + "."
			}
			needs = append(needs, Need{Text: app + ": " + what + " is not proven. " + proven, Command: build.Name + " backups " + app})
		}
	}
	return needs
}

// running reports whether at least one container of the app runs.
func (a *apps) running(ctx context.Context, app string) bool {
	ids, err := a.output(ctx, "docker", "compose", "-p", a.projectOf(app), "ps", "--status", "running", "--quiet")
	return err == nil && strings.TrimSpace(ids) != ""
}
