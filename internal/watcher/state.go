package watcher

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
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
	// kept is how many outages, restore checks and unsent alerts the
	// state keeps.
	kept = 100
)

// state is all a watcher knows. It is saved as JSON.
type state struct {
	Servers map[string]*server `json:"servers"`
	// Domains are the domains of running apps, by name.
	Domains map[string]*domain `json:"domains"`
	// Outages and Checks are the history, oldest first.
	Outages []outage `json:"outages"`
	Checks  []check  `json:"checks"`
	// Unsent are alerts a destination did not take yet; each check tries
	// them again.
	Unsent []unsent `json:"unsent"`
}

// unsent is one alert for one destination.
type unsent struct {
	To      alert.Destination `json:"to"`
	Message alert.Message     `json:"message"`
}

// server is the newest report of a server and when it came.
type server struct {
	Report   report.Report `json:"report"`
	Received time.Time     `json:"received,omitzero"`
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
	Turning time.Time `json:"turning,omitzero"`
	// Since is when Down last changed.
	Since time.Time `json:"since,omitzero"`
	// Reason is why the last failed visit failed.
	Reason string `json:"reason"`
	// Alerted is true from the down alert to the up alert.
	Alerted bool `json:"alerted"`
	// Visited is when the last visit ran; zero before the first.
	Visited time.Time `json:"visited,omitzero"`
}

// outage is a server that stopped reporting (Domain is "") or a domain
// that was down. To is zero while it lasts.
type outage struct {
	Server string    `json:"server"`
	App    string    `json:"app,omitempty"`
	Domain string    `json:"domain,omitempty"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to,omitzero"`
	Reason string    `json:"reason,omitempty"`
}

// check is one restore check a daemon reported.
type check struct {
	Server   string    `json:"server"`
	App      string    `json:"app"`
	Database string    `json:"database"`
	At       time.Time `json:"at"`
	OK       bool      `json:"ok"`
	Copy     time.Time `json:"copy,omitzero"`
	Error    string    `json:"error,omitempty"`
}

// newState is a watcher that knows nothing yet.
func newState() *state {
	return &state{Servers: map[string]*server{}, Domains: map[string]*domain{}}
}

// parseState reads a state saved before; nothing saved is an empty state.
func parseState(saved []byte) (*state, error) {
	s := newState()
	if len(saved) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(saved, s); err != nil {
		return nil, err
	}
	if s.Servers == nil {
		s.Servers = map[string]*server{}
	}
	if s.Domains == nil {
		s.Domains = map[string]*domain{}
	}
	return s, nil
}

// forget drops what the watcher no longer watches: servers with their
// domains, and unsent alerts to a destination that is gone.
func (s *state) forget(servers []string, own []alert.Destination, now time.Time) {
	for name := range s.Servers {
		if !slices.Contains(servers, name) {
			delete(s.Servers, name)
		}
	}
	s.syncDomains(now)
	s.forgetUnsent(own)
}

// forgetUnsent drops the unsent alerts to a destination that is gone:
// neither one of the watcher's own nor in the last report of a server.
func (s *state) forgetUnsent(own []alert.Destination) {
	s.Unsent = slices.DeleteFunc(s.Unsent, func(u unsent) bool {
		if slices.Contains(own, u.To) {
			return false
		}
		for _, srv := range s.Servers {
			if slices.Contains(srv.Report.Alerts, u.To) {
				return false
			}
		}
		return true
	})
}

// destinations are where an alert about server goes: where its last
// report says, and the watcher's own, each once.
func (s *state) destinations(server string, own []alert.Destination) []alert.Destination {
	var out []alert.Destination
	if srv := s.Servers[server]; srv != nil {
		out = append(out, srv.Report.Alerts...)
	}
	for _, d := range own {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
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
	s.Checks = s.Checks[max(0, len(s.Checks)-kept):]
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
// without an alert: it is gone, not up. When two servers report one
// domain, the one that had it keeps it, else the first by name.
func (s *state) syncDomains(now time.Time) {
	want := map[string]*domain{}
	for _, name := range slices.Sorted(maps.Keys(s.Servers)) {
		for _, app := range s.Servers[name].Report.Apps {
			if !app.Running {
				continue
			}
			for _, d := range app.Domains {
				if _, taken := want[d]; taken && !s.owns(d, name, app.Name) {
					continue
				}
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

// owns says whether a domain is visited for this server and app now.
func (s *state) owns(domain, server, app string) bool {
	d := s.Domains[domain]
	return d != nil && d.Server == server && d.App == app
}

// silent says whether a server that last reported at received, every
// everySeconds, is silent at now. The interval counts from 1 minute to
// 1 hour. Time before the watcher started does not count, so a watcher
// that was off does not call every server silent.
func silent(received, started, now time.Time, everySeconds int) bool {
	if received.IsZero() {
		return false
	}
	every := min(max(time.Duration(everySeconds)*time.Second, time.Minute), time.Hour)
	return now.Sub(later(received, started)) > silentAfter*every
}

// later is the later of two times.
func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// visited counts one visit of d at at: inARow visits in a row that say the
// opposite turn it.
func (d *domain) visited(up bool, reason string, at time.Time) {
	d.Visited = at
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
// or came up.
func (s *state) alerts(started, now time.Time) []alert.Message {
	var out []alert.Message
	for _, name := range slices.Sorted(maps.Keys(s.Servers)) {
		if m, ok := s.serverAlert(name, started, now); ok {
			out = append(out, m)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(s.Domains)) {
		if m, ok := s.domainAlert(name, now); ok {
			out = append(out, m)
		}
	}
	return out
}

// serverAlert is the alert of a server that just went silent or just came
// back, if it did.
func (s *state) serverAlert(name string, started, now time.Time) (alert.Message, bool) {
	srv := s.Servers[name]
	quiet := silent(srv.Received, started, now, srv.Report.EverySeconds)
	if quiet && !srv.Silent {
		srv.Silent = true
		s.addOutage(outage{Server: name, From: srv.Received})
		return alert.Message{
			Server: name,
			Title:  name + " stopped reporting",
			Text: "No report since " + clock(srv.Received) + ". The server, its network or moin on it is down." + appsOf(srv.Report) +
				"\nLook: ssh " + name + ", or the console of your provider.",
		}, true
	}
	// Back only with a report since the start: a watcher that restarts
	// does not call a silent server back.
	if !quiet && srv.Silent && srv.Received.After(started) {
		srv.Silent = false
		from := s.end(name, "", now)
		return alert.Message{Server: name, Title: name + " reports again", OK: true, Text: "It was silent for " + spoken(now.Sub(from)) + "."}, true
	}
	return alert.Message{}, false
}

// domainAlert is the alert of a domain that just went down or came up, if
// it did. A domain of a silent server does not alert: the silence says
// more. When the server is back and the domain still down, it alerts then.
func (s *state) domainAlert(name string, now time.Time) (alert.Message, bool) {
	d := s.Domains[name]
	srv := s.Servers[d.Server]
	if d.Down && !d.Alerted && srv != nil && !srv.Silent {
		d.Alerted = true
		s.addOutage(outage{Server: d.Server, App: d.App, Domain: name, From: d.Since, Reason: d.Reason})
		return alert.Message{
			Server: d.Server, App: d.App,
			Title: name + " is down",
			Text: "No good answer from outside since " + clock(d.Since) + ": " + d.Reason + ".\n" +
				d.Server + " still reports, so look at the app: " + build.Name + " logs " + d.App,
		}, true
	}
	if !d.Down && d.Alerted {
		d.Alerted = false
		from := s.end(d.Server, name, now)
		return alert.Message{Server: d.Server, App: d.App, Title: name + " is up again", OK: true, Text: "It was down for " + spoken(now.Sub(from)) + "."}, true
	}
	return alert.Message{}, false
}

// addOutage adds an outage to the history. Past kept, the oldest outage
// that ended goes; an open one always stays.
func (s *state) addOutage(o outage) {
	s.Outages = append(s.Outages, o)
	for len(s.Outages) > kept {
		i := slices.IndexFunc(s.Outages, func(o outage) bool { return !o.To.IsZero() })
		if i < 0 {
			return
		}
		s.Outages = slices.Delete(s.Outages, i, i+1)
	}
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

// appsOf names the first apps of a report, for the silence alert. A
// short list keeps the alert inside what every chat takes.
func appsOf(r report.Report) string {
	const shown = 10
	if len(r.Apps) == 0 {
		return ""
	}
	var names []string
	for _, app := range r.Apps[:min(len(r.Apps), shown)] {
		names = append(names, app.Name)
	}
	more := ""
	if len(r.Apps) > shown {
		more = fmt.Sprintf(" and %d more", len(r.Apps)-shown)
	}
	return "\nApps on it: " + strings.Join(names, ", ") + more + "."
}

// clock is a moment as the alerts and the page print it.
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

// plural is n and the word, with an s unless n is 1.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
