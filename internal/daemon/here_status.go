package daemon

import (
	"context"
	"strings"

	"github.com/axadrn/moin/internal/build"
)

// Status is what runs on this server, for `moin` and `moin apps` on your
// laptop.
type Status struct {
	Version  string      `json:"version"`
	Apps     []AppStatus `json:"apps"`
	Watchers []string    `json:"watchers"`
	// PublicSSH: port 22 is open to the internet.
	PublicSSH bool `json:"public_ssh"`
	// KnocksToday is how many connections the firewall dropped today.
	KnocksToday int64 `json:"knocks_today"`
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

// running reports whether at least one container of the app runs.
func (a *apps) running(ctx context.Context, app string) bool {
	ids, err := a.output(ctx, "docker", "compose", "-p", a.projectOf(app), "ps", "--status", "running", "--quiet")
	return err == nil && strings.TrimSpace(ids) != ""
}
