package watcher

import (
	"slices"
	"strconv"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
)

// View is everything the page of a watcher shows, ready to print: what
// needs you, every server and its apps, recent outages, the last proven
// restores. moin cloud prints it on its own page.
type View struct {
	Name     string
	NeedsYou []string
	Servers  []ServerView
	Outages  []OutageView
	Checks   []CheckView
}

// ServerView is one server with its apps. ServerView, AppView and
// LineView each have a Problem: one sentence for "Needs you" when it is
// red, empty when it is fine.
type ServerView struct {
	Name, Version, State, Problem string
	OK                            bool
	Apps                          []AppView
	// Knocks are what the server's firewall dropped, as its last report
	// said; nil while it counted none.
	Knocks *report.Knocks
}

// AppView is one app of a server.
type AppView struct {
	Name, State, Problem string
	OK                   bool
	Domains, Databases   []LineView
}

// LineView is one domain or one database of an app.
type LineView struct {
	Name, State, Problem string
}

// OutageView is one outage: a silent server or a domain that was down.
type OutageView struct {
	What, From, Lasted, Reason string
	Open                       bool
}

// CheckView is one restore check a daemon reported.
type CheckView struct {
	What, At, Result string
	OK               bool
}

// View is what the watcher's page shows now.
func (w *Watcher) View() View {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.view(time.Now())
}

// view is what the page shows at now. The caller holds w.mu.
func (w *Watcher) view(now time.Time) View {
	v := View{Name: build.Name}
	for _, name := range w.servers {
		server := w.serverView(name, now)
		v.Servers = append(v.Servers, server)
		v.NeedsYou = append(v.NeedsYou, problems(server)...)
	}
	for _, o := range slices.Backward(w.state.Outages) {
		v.Outages = append(v.Outages, outageLine(o, now))
	}
	for _, c := range slices.Backward(w.state.Checks) {
		v.Checks = append(v.Checks, checkLine(c))
	}
	return v
}

// ServerView is one server with its apps, from its newest report.
func (w *Watcher) serverView(name string, now time.Time) ServerView {
	srv := w.state.Servers[name]
	if srv == nil {
		return ServerView{Name: name, State: "no report yet",
			Problem: name + " has not reported yet. On your laptop: " + w.Connect(name)}
	}
	server := ServerView{Name: name, Version: srv.Report.Version, State: "last report " + ago(now, srv.Received), Knocks: srv.Report.Knocks}
	if srv.Silent {
		server.Problem = name + " stopped reporting " + ago(now, srv.Received) + ". Look: ssh " + name
	}
	server.OK = server.Problem == ""
	for _, app := range srv.Report.Apps {
		a := w.appView(name, app, now)
		server.OK = server.OK && a.OK
		server.Apps = append(server.Apps, a)
	}
	return server
}

// AppView is one app: running or not, its domains as seen from outside,
// its databases with the last copy and the last proven restore.
func (w *Watcher) appView(server string, app report.App, now time.Time) AppView {
	a := AppView{Name: app.Name, State: "v" + strconv.Itoa(app.Release) + " running"}
	if !app.Running {
		a.State = "v" + strconv.Itoa(app.Release) + " stopped"
		a.Problem = app.Name + " on " + server + " is stopped."
	}
	a.OK = a.Problem == ""
	for _, name := range app.Domains {
		line := w.domainLine(name, app.Running)
		a.OK = a.OK && line.Problem == ""
		a.Domains = append(a.Domains, line)
	}
	for _, db := range app.Databases {
		line := databaseLine(server, app.Name, db, now)
		a.OK = a.OK && line.Problem == ""
		a.Databases = append(a.Databases, line)
	}
	return a
}

// domainLine says whether a domain answers from outside.
func (w *Watcher) domainLine(name string, running bool) LineView {
	d := w.state.Domains[name]
	switch {
	case !running || d == nil:
		return LineView{Name: name, State: "not visited"}
	case d.Visited.IsZero():
		return LineView{Name: name, State: "not visited yet"}
	case d.Down:
		return LineView{Name: name, State: "down: " + d.Reason, Problem: name + " is down: " + d.Reason + "."}
	}
	return LineView{Name: name, State: "up"}
}

// databaseLine says when the last copy was made and which copy a restore
// last proved. A failed check or a backup error makes it red.
func databaseLine(server, app string, db report.Database, now time.Time) LineView {
	line := LineView{Name: db.Name + " (" + db.Kind + ")", State: "copy " + ago(now, db.LastCopy) + ", restore proven " + ago(now, db.CheckedCopy)}
	where := db.Name + " of " + app + " on " + server
	if db.LastCopy.IsZero() {
		line.Problem = where + " has no copy yet. Set up backups: " + build.Name + " bucket set " + server
	}
	if !db.LastCheck.IsZero() && !db.CheckOK {
		line.State = "restore check failed " + ago(now, db.LastCheck) + ", " + line.State
		line.Problem = "The restore check of " + where + " failed " + ago(now, db.LastCheck) + "."
	}
	if db.Error != "" {
		line.State += ". " + db.Error
		line.Problem = where + ": " + db.Error
	}
	return line
}

// problems lists what is red on a server, one sentence each.
func problems(server ServerView) []string {
	var out []string
	if server.Problem != "" {
		out = append(out, server.Problem)
	}
	for _, app := range server.Apps {
		if app.Problem != "" {
			out = append(out, app.Problem)
		}
		for _, line := range slices.Concat(app.Domains, app.Databases) {
			if line.Problem != "" {
				out = append(out, line.Problem)
			}
		}
	}
	return out
}

// outageLine is one outage, newest first on the page.
func outageLine(o outage, now time.Time) OutageView {
	what := o.Server + " stopped reporting"
	if o.Domain != "" {
		what = o.Domain + " down (" + o.App + " on " + o.Server + ")"
	}
	line := OutageView{What: what, From: clock(o.From), Reason: o.Reason, Open: o.To.IsZero()}
	if line.Open {
		line.Lasted = "still, " + spoken(now.Sub(o.From))
	} else {
		line.Lasted = spoken(o.To.Sub(o.From))
	}
	return line
}

// checkLine is one restore check, newest first on the page.
func checkLine(c check) CheckView {
	line := CheckView{What: c.Database + " of " + c.App + " on " + c.Server, At: clock(c.At), OK: c.OK}
	if c.OK {
		line.Result = "restored the copy of " + clock(c.Copy)
	} else {
		line.Result = "failed: " + c.Error
	}
	return line
}

// ago is how long ago t was, or "never".
func ago(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return spoken(now.Sub(t)) + " ago"
}
