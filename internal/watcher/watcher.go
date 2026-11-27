package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/report"
)

const (
	// The most a report may hold: apps, and domains and databases per app.
	maxApps      = 100
	maxDomains   = 50
	maxDatabases = 20
	// maxError is how much of a backup error the watcher keeps.
	maxError = 200
)

// Watcher is one watcher: the servers it watches, where its alerts go,
// and all it knows about them. `moin watcher run` keeps one; moin cloud
// keeps one per workspace. It is safe to use from many goroutines.
type Watcher struct {
	// Send sends one alert to one destination. New sets it to alert.Send;
	// moin cloud puts its own in front, which also sends mail.
	Send func(context.Context, alert.Destination, alert.Message) error

	// servers are the names of the servers it watches, sorted.
	servers []string
	alerts  []alert.Destination
	// started is when this watcher began to watch: silence before it does
	// not count.
	started time.Time

	mu    sync.Mutex
	state *state
	// dirty is true when state changed since the last save.
	dirty bool
}

// New is a watcher of servers that alerts to alerts. saved is what Save
// wrote before, or nothing for a watcher that knows nothing yet. What it
// knew of servers no longer in the list is forgotten.
func New(servers []string, alerts []alert.Destination, saved []byte) (*Watcher, error) {
	return newAt(servers, alerts, saved, time.Now())
}

// newAt is New, started at now.
func newAt(servers []string, alerts []alert.Destination, saved []byte, now time.Time) (*Watcher, error) {
	s, err := parseState(saved)
	if err != nil {
		return nil, fmt.Errorf("the saved state is not what a watcher saves: %w", err)
	}
	s.forget(servers, alerts, now)
	return &Watcher{Send: alert.Send, servers: slices.Sorted(slices.Values(servers)), alerts: slices.Clone(alerts), started: now, state: s}, nil
}

// Receive keeps the report of a server. It refuses a report that names
// another server, or one too big or with odd names.
func (w *Watcher) Receive(server string, r report.Report) error {
	if r.Server != server {
		return fmt.Errorf("this token is for server %s", server)
	}
	if !slices.Contains(w.servers, server) {
		return fmt.Errorf("this watcher does not watch %s", server)
	}
	if err := checkReport(&r); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.receive(server, r, time.Now())
	w.dirty = true
	return nil
}

// Tick finds what changed, sends its alerts once each, and tries again
// the alerts a destination did not take. Call it every few seconds.
func (w *Watcher) Tick(ctx context.Context) {
	w.mu.Lock()
	for _, m := range w.state.alerts(w.started, time.Now()) {
		slog.Info("alert", "server", m.Server, "app", m.App, "title", m.Title)
		for _, d := range w.alerts {
			w.state.Unsent = append(w.state.Unsent, unsent{To: d, Message: m})
		}
	}
	todo := w.state.Unsent
	w.state.Unsent = nil
	w.mu.Unlock()

	failed := w.sendAll(ctx, todo)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.Unsent = append(failed, w.state.Unsent...)
	w.state.Unsent = w.state.Unsent[max(0, len(w.state.Unsent)-kept):]
	w.dirty = w.dirty || len(todo) > 0
}

// sendAll sends each alert and returns the ones that did not go out.
// After one failure a destination gets nothing more this round, so a dead
// one does not hold up the others.
func (w *Watcher) sendAll(ctx context.Context, todo []unsent) []unsent {
	var failed []unsent
	down := map[alert.Destination]bool{}
	for _, u := range todo {
		if down[u.To] {
			failed = append(failed, u)
			continue
		}
		if err := w.Send(ctx, u.To, u.Message); err != nil {
			slog.Error("alert not sent, trying again soon", "kind", u.To.Kind, "title", u.Message.Title, "error", err)
			down[u.To] = true
			failed = append(failed, u)
		}
	}
	return failed
}

// Visit visits every domain of the running apps with visitor, all at
// once, and counts what each visit said. Call it every minute.
func (w *Watcher) Visit(ctx context.Context, visitor *Visitor) {
	w.mu.Lock()
	names := slices.Sorted(maps.Keys(w.state.Domains))
	w.mu.Unlock()
	failures := make([]string, len(names))
	var wait sync.WaitGroup
	for i, name := range names {
		wait.Go(func() { failures[i] = visitor.Visit(ctx, name) })
	}
	wait.Wait()
	if ctx.Err() != nil {
		return
	}
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, name := range names {
		if d := w.state.Domains[name]; d != nil {
			d.visited(failures[i] == "", failures[i], now)
		}
	}
	w.dirty = true
}

// Save hands what the watcher knows to write, as bytes, when it changed
// since the last save. When write fails, the next Save tries again.
func (w *Watcher) Save(write func([]byte) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.dirty {
		return nil
	}
	data, err := json.MarshalIndent(w.state, "", "  ")
	if err != nil {
		return err
	}
	if err := write(data); err != nil {
		return err
	}
	w.dirty = false
	return nil
}

// hostname is a plain domain name: no port, path, user or IP address.
var hostname = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// checkReport refuses a report that is too big or names things oddly, and
// cuts long backup errors. The watcher visits the domains of a report, so
// each must be a plain public name.
func checkReport(r *report.Report) error {
	if len(r.Apps) > maxApps {
		return fmt.Errorf("more than %d apps", maxApps)
	}
	for i := range r.Apps {
		app := &r.Apps[i]
		if !serverName.MatchString(app.Name) {
			return fmt.Errorf("%q is not an app name", app.Name)
		}
		if len(app.Domains) > maxDomains || len(app.Databases) > maxDatabases {
			return fmt.Errorf("%s has more than %d domains or %d databases", app.Name, maxDomains, maxDatabases)
		}
		for _, d := range app.Domains {
			if len(d) > 253 || !hostname.MatchString(d) || net.ParseIP(d) != nil {
				return fmt.Errorf("%q of %s is not a plain domain name", d, app.Name)
			}
		}
		for j := range app.Databases {
			db := &app.Databases[j]
			if db.Name == "" || len(db.Name) > 200 || !utf8.ValidString(db.Name) || strings.ContainsFunc(db.Name, unicode.IsControl) {
				return fmt.Errorf("%q of %s is not a database name", db.Name, app.Name)
			}
			db.Error = clip(db.Error, maxError)
		}
	}
	return nil
}

// clip cuts text to at most n bytes, on a whole character.
func clip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n] + "…"
}
