package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
)

const (
	// The most a report may hold: apps, and domains and databases per app.
	maxApps      = 100
	maxDomains   = 50
	maxDatabases = 20
	// maxAlerts is how many alert destinations a report may hold, and
	// maxAlertField how long a URL or token in one may be.
	maxAlerts     = 10
	maxAlertField = 2000
	// maxError is how much of a backup error the watcher keeps.
	maxError = 200
)

// Watcher is one watcher: the servers it watches, where its alerts go,
// and all it knows about them. The alerts of a server go where its last
// report says, and to the watcher's own destinations. `moin watcher run` keeps one; moin cloud
// keeps one per workspace. It is safe to use from many goroutines.
type Watcher struct {
	// Send sends one alert to one of the watcher's own destinations. New
	// sets it to alert.Send; moin cloud's sends a mail.
	Send func(context.Context, alert.Destination, alert.Message) error
	// SendReported sends one alert to a destination a server's report
	// named. New sets it to SendPublic; moin cloud puts its trial in front.
	SendReported func(context.Context, alert.Destination, alert.Message) error
	// Connect is the command that connects a server to this watcher, for
	// the page of a server that has not reported yet. moin cloud says
	// its own.
	Connect func(server string) string

	// servers are the names of the servers it watches, sorted.
	servers []string
	// alerts are the watcher's own destinations: all there is for a
	// server whose report names none.
	alerts []alert.Destination
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
	return &Watcher{Send: alert.Send, SendReported: SendPublic, Connect: connectLine, servers: slices.Sorted(slices.Values(servers)), alerts: slices.Clone(alerts), started: now, state: s}, nil
}

// connectLine is how a server is connected to a watcher you run.
func connectLine(server string) string {
	return build.Name + " watcher connect " + server + " <watcher url> <token>"
}

// SetServers changes the servers the watcher watches. What it knew of a
// server no longer in the list is forgotten.
func (w *Watcher) SetServers(servers []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.servers = slices.Sorted(slices.Values(servers))
	w.state.forget(w.servers, w.alerts, time.Now())
	w.dirty = true
}

// Receive keeps the report of a server. It refuses a report that names
// another server, or one too big or with odd names.
func (w *Watcher) Receive(server string, r report.Report) error {
	if r.Server != server {
		return fmt.Errorf("this token is for server %s", server)
	}
	if err := checkReport(&r); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !slices.Contains(w.servers, server) {
		return fmt.Errorf("this watcher does not watch %s", server)
	}
	w.state.receive(server, r, time.Now())
	w.state.forgetUnsent(w.alerts)
	w.dirty = true
	return nil
}

// Tick finds what changed, sends its alerts once each, and tries again
// the alerts a destination did not take. Call it every few seconds.
func (w *Watcher) Tick(ctx context.Context) {
	w.mu.Lock()
	for _, m := range w.state.alerts(w.started, time.Now()) {
		slog.Info("alert", "server", m.Server, "app", m.App, "title", m.Title)
		for _, d := range w.state.destinations(m.Server, w.alerts) {
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
// Every destination is sent to at the same time, so a slow one only
// makes itself late.
func (w *Watcher) sendAll(ctx context.Context, todo []unsent) []unsent {
	var destinations []alert.Destination
	alerts := map[alert.Destination][]unsent{}
	for _, u := range todo {
		if alerts[u.To] == nil {
			destinations = append(destinations, u.To)
		}
		alerts[u.To] = append(alerts[u.To], u)
	}
	failed := make([][]unsent, len(destinations))
	var wait sync.WaitGroup
	for i, to := range destinations {
		wait.Go(func() { failed[i] = w.sendInOrder(ctx, alerts[to]) })
	}
	wait.Wait()
	return slices.Concat(failed...)
}

// sendInOrder sends the alerts of one destination, oldest first. At the
// first that does not go out it stops, and returns that one and the rest.
func (w *Watcher) sendInOrder(ctx context.Context, alerts []unsent) []unsent {
	for i, u := range alerts {
		if err := w.send(ctx, u); err != nil {
			slog.Error("alert not sent, trying again soon", "kind", u.To.Kind, "title", u.Message.Title, "error", err)
			return alerts[i:]
		}
	}
	return nil
}

// send sends one alert through one of two doors. A destination of the
// watcher's own list was written by its owner, so it may be anywhere. Any
// other came from a report, and a report cannot make the watcher knock on
// its own network.
func (w *Watcher) send(ctx context.Context, u unsent) error {
	if slices.Contains(w.alerts, u.To) {
		return w.Send(ctx, u.To, u.Message)
	}
	return w.SendReported(ctx, u.To, u.Message)
}

// public sends to public addresses only, and follows no redirects.
var public = alert.NewSender(RefusePrivate)

// SendPublic sends one alert to a destination a server's report named:
// over https only, and never to a private address.
func SendPublic(ctx context.Context, d alert.Destination, m alert.Message) error {
	if d.URL != "" && !strings.HasPrefix(d.URL, "https://") {
		return errors.New("the alert url must start with https://")
	}
	return public.Send(ctx, d, m)
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

// checkReport refuses a report that is too big or names things oddly. It
// cuts long backup errors and leaves out the alert destinations a watcher
// never sends to. The watcher visits the domains of a report, so each
// must be a plain public name.
func checkReport(r *report.Report) error {
	if len(r.Apps) > maxApps {
		return fmt.Errorf("more than %d apps", maxApps)
	}
	if len(r.Alerts) > maxAlerts {
		return fmt.Errorf("more than %d alerts", maxAlerts)
	}
	for _, d := range r.Alerts {
		if len(d.Kind)+len(d.URL)+len(d.Token)+len(d.ChatID) > maxAlertField {
			return errors.New("an alert is too long")
		}
	}
	// A destination this watcher never sends to is left out: it would
	// fail again every few seconds, and fill the list of unsent alerts.
	r.Alerts = slices.DeleteFunc(r.Alerts, func(d report.Alert) bool { return !sendable(d) })
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

// sendable says whether a watcher sends to a destination a report named.
// It does not to a kind it does not know, like one of a newer moin, to a
// url without https, or to a private address written out. A name that
// points at a private address is refused later, when the alert goes out.
func sendable(d report.Alert) bool {
	if alert.Check(d) != nil {
		return false
	}
	if d.URL == "" {
		return true
	}
	address, err := url.Parse(d.URL)
	return err == nil && address.Scheme == "https" && !privateAddress(address.Hostname())
}

// privateAddress says whether host is an IP address RefusePrivate stops.
// A name is not: only a connection shows where it points.
func privateAddress(host string) bool {
	return net.ParseIP(host) != nil && RefusePrivate("tcp", net.JoinHostPort(host, "443"), nil) != nil
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
