package watcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/report"
)

const (
	// silentAfter is how many report intervals a server may miss before it
	// counts as silent.
	silentAfter = 3
	// inARow is how many visits in a row must say the opposite before a
	// domain goes down or comes back: a restart or a short hiccup changes
	// nothing.
	inARow = 3
	// kept is how many outages and restore checks the history keeps.
	kept = 100
)

// state is all the watcher knows, in one JSON file.
type state struct {
	Servers map[string]*server `json:"servers"`
	// Domains are the domains of running apps, by name.
	Domains map[string]*domain `json:"domains"`
	// Outages and Checks are the history, oldest first.
	Outages []outage `json:"outages"`
	Checks  []check  `json:"checks"`
}

// server is the newest report of a server and when it came.
type server struct {
	Report   report.Report `json:"report"`
	Received time.Time     `json:"received"`
	// Silent is true from the alert that the server stopped reporting to
	// the one that it is back.
	Silent bool `json:"silent"`
}

// domain is one domain the watcher visits from outside.
type domain struct {
	Server string `json:"server"`
	App    string `json:"app"`
	// Down is what three visits in a row said.
	Down bool `json:"down"`
	// Against counts the visits in a row that say the opposite of Down,
	// and Turning is when the first of them ran.
	Against int       `json:"against"`
	Turning time.Time `json:"turning"`
	// Since is when Down last changed.
	Since time.Time `json:"since"`
	// Reason is why the last failed visit failed.
	Reason string `json:"reason"`
	// Alerted is true from the down alert to the up alert.
	Alerted bool `json:"alerted"`
}

// outage is a server that stopped reporting (Domain is "") or a domain
// that was down. To is zero while it lasts.
type outage struct {
	Server string    `json:"server"`
	App    string    `json:"app,omitempty"`
	Domain string    `json:"domain,omitempty"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Reason string    `json:"reason,omitempty"`
}

// check is one restore check a daemon reported.
type check struct {
	Server   string    `json:"server"`
	App      string    `json:"app"`
	Database string    `json:"database"`
	At       time.Time `json:"at"`
	OK       bool      `json:"ok"`
	Copy     time.Time `json:"copy"`
	Error    string    `json:"error,omitempty"`
}

func newState() *state {
	return &state{Servers: map[string]*server{}, Domains: map[string]*domain{}}
}

// loadState reads the state file; a missing one is an empty state.
func loadState(path string) (*state, error) {
	s := newState()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Servers == nil {
		s.Servers = map[string]*server{}
	}
	if s.Domains == nil {
		s.Domains = map[string]*domain{}
	}
	return s, nil
}

// save writes the state file whole, or not at all.
func (s *state) save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// forget drops the servers the config no longer has, and their domains.
func (s *state) forget(config Config) {
	for name := range s.Servers {
		if _, ok := config.Servers[name]; !ok {
			delete(s.Servers, name)
		}
	}
	s.syncDomains(time.Now())
}

// receive keeps a report of server name, received at now: its restore
// checks go into the history, its domains are visited from now on.
func (s *state) receive(name string, r report.Report, now time.Time) {
	before := s.Servers[name]
	if before == nil {
		before = &server{}
		s.Servers[name] = before
	}
	for _, app := range r.Apps {
		for _, db := range app.Databases {
			if db.LastCheck.IsZero() || !db.LastCheck.After(lastCheck(before.Report, app.Name, db.Name)) {
				continue
			}
			s.Checks = append(s.Checks, check{Server: name, App: app.Name, Database: db.Name, At: db.LastCheck, OK: db.CheckOK, Copy: db.CheckedCopy, Error: db.Error})
		}
	}
	s.Checks = newest(s.Checks)
	before.Report, before.Received = r, now
	s.syncDomains(now)
}

// lastCheck is when the restore check of one database last ran, in r.
func lastCheck(r report.Report, app, database string) time.Time {
	for _, a := range r.Apps {
		if a.Name != app {
			continue
		}
		for _, db := range a.Databases {
			if db.Name == database {
				return db.LastCheck
			}
		}
	}
	return time.Time{}
}

// syncDomains visits the domains of the running apps of every server, and
// forgets the others. A forgotten domain that was down ends its outage
// without an alert: it is gone, not up.
func (s *state) syncDomains(now time.Time) {
	want := map[string]*domain{}
	for name, srv := range s.Servers {
		for _, app := range srv.Report.Apps {
			if !app.Running {
				continue
			}
			for _, d := range app.Domains {
				want[d] = &domain{Server: name, App: app.Name}
			}
		}
	}
	for name, d := range s.Domains {
		if w, ok := want[name]; ok && w.Server == d.Server && w.App == d.App {
			continue
		}
		if d.Alerted {
			s.end(d.Server, name, now)
		}
		delete(s.Domains, name)
	}
	for name, d := range want {
		if s.Domains[name] == nil {
			d.Since = now
			s.Domains[name] = d
		}
	}
}

// silent says whether a server that last reported at received, every
// every, is silent at now. Time before the watcher started does not count,
// so a watcher that was off does not call every server silent.
func silent(received, started, now time.Time, every time.Duration) bool {
	if received.IsZero() {
		return false
	}
	if every <= 0 {
		every = time.Minute
	}
	return now.Sub(later(received, started)) > silentAfter*every
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// visited counts one visit of d at at: inARow visits in a row that say the
// opposite turn it.
func (d *domain) visited(up bool, reason string, at time.Time) {
	if !up {
		d.Reason = reason
	}
	if up != d.Down {
		d.Against = 0
		return
	}
	if d.Against == 0 {
		d.Turning = at
	}
	d.Against++
	if d.Against >= inARow {
		d.Down, d.Since, d.Against = !up, d.Turning, 0
	}
}

// alerts turns what changed since the last call into alerts, once per
// event: a server that went silent or came back, a domain that went down
// or came up. A domain of a silent server does not alert: the silence
// says more, and when the server is back and the domain still down, it
// alerts then.
func (s *state) alerts(started, now time.Time) []alert.Message {
	var out []alert.Message
	for _, name := range sortedKeys(s.Servers) {
		srv := s.Servers[name]
		quiet := silent(srv.Received, started, now, srv.Report.Every)
		switch {
		case quiet && !srv.Silent:
			srv.Silent = true
			s.Outages = newest(append(s.Outages, outage{Server: name, From: srv.Received}))
			out = append(out, alert.Message{Server: name, Title: name + " stopped reporting",
				Text: "No report since " + clock(srv.Received) + ". The server, its network or moin on it is down." + appsOf(srv.Report)})
		case !quiet && srv.Silent:
			srv.Silent = false
			from := s.end(name, "", now)
			out = append(out, alert.Message{Server: name, Title: name + " reports again", OK: true,
				Text: "It was silent for " + spoken(now.Sub(from)) + "."})
		}
	}
	for _, name := range sortedKeys(s.Domains) {
		d := s.Domains[name]
		srv := s.Servers[d.Server]
		switch {
		case d.Down && !d.Alerted && srv != nil && !srv.Silent:
			d.Alerted = true
			s.Outages = newest(append(s.Outages, outage{Server: d.Server, App: d.App, Domain: name, From: d.Since, Reason: d.Reason}))
			out = append(out, alert.Message{Server: d.Server, App: d.App, Title: name + " is down",
				Text: "Down from outside since " + clock(d.Since) + ": " + d.Reason + "."})
		case !d.Down && d.Alerted:
			d.Alerted = false
			from := s.end(d.Server, name, now)
			out = append(out, alert.Message{Server: d.Server, App: d.App, Title: name + " is up again", OK: true,
				Text: "It was down for " + spoken(now.Sub(from)) + "."})
		}
	}
	return out
}

// end ends the open outage of a server or one of its domains, and says
// when it began.
func (s *state) end(server, domain string, now time.Time) time.Time {
	for i := len(s.Outages) - 1; i >= 0; i-- {
		o := &s.Outages[i]
		if o.Server == server && o.Domain == domain && o.To.IsZero() {
			o.To = now
			return o.From
		}
	}
	return now
}

// newest keeps the last kept entries.
func newest[T any](list []T) []T {
	if len(list) > kept {
		return slices.Clone(list[len(list)-kept:])
	}
	return list
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// appsOf names the apps of a report, for the silence alert.
func appsOf(r report.Report) string {
	var names []string
	for _, app := range r.Apps {
		names = append(names, app.Name)
	}
	if len(names) == 0 {
		return ""
	}
	return "\nApps on it: " + strings.Join(names, ", ") + "."
}

func clock(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

// spoken is a duration as a person says it: 40 seconds, 12 minutes, 3 hours.
func spoken(d time.Duration) string {
	switch {
	case d < time.Minute:
		return plural(int(d.Seconds()), "second")
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour")
	}
	return plural(int(d.Hours()/24), "day")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
