package watcher

import (
	"crypto/subtle"
	"html/template"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
)

// page is the one page: what needs you, every server and its apps, the
// last proven restore, recent outages. It only reads: nothing on it
// changes anything. It asks for the password of the config, any user name.
func (w *watcher) page(rw http.ResponseWriter, r *http.Request) {
	_, password, ok := r.BasicAuth()
	if !ok || subtle.ConstantTimeCompare([]byte(password), []byte(w.config.Password)) != 1 {
		rw.Header().Set("WWW-Authenticate", `Basic realm="`+build.Name+` watcher", charset="UTF-8"`)
		http.Error(rw, "the watcher asks for its password", http.StatusUnauthorized)
		return
	}
	w.mu.Lock()
	view := w.view(time.Now())
	w.mu.Unlock()
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	if err := pageTemplate.Execute(rw, view); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
	}
}

// pageView is everything the page shows, ready to print.
type pageView struct {
	Name     string
	NeedsYou []string
	Servers  []serverView
	Outages  []outageView
	Checks   []checkView
}

// serverView, appView and lineView each have a Problem: one sentence
// for "Needs you" when it is red, empty when it is fine.
type serverView struct {
	Name, Version, State, Problem string
	OK                            bool
	Apps                          []appView
}

type appView struct {
	Name, State, Problem string
	OK                   bool
	Domains, Databases   []lineView
}

// lineView is one domain or one database.
type lineView struct {
	Name, State, Problem string
}

type outageView struct {
	What, From, Lasted, Reason string
	Open                       bool
}

type checkView struct {
	What, At, Result string
	OK               bool
}

// view is what the page shows at now. The caller holds w.mu.
func (w *watcher) view(now time.Time) pageView {
	v := pageView{Name: build.Name}
	for _, name := range slices.Sorted(maps.Keys(w.config.Servers)) {
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

// serverView is one server with its apps, from its newest report.
func (w *watcher) serverView(name string, now time.Time) serverView {
	srv := w.state.Servers[name]
	if srv == nil {
		return serverView{Name: name, State: "no report yet",
			Problem: name + " has not reported yet. On your laptop: " + build.Name + " watcher connect " + name + " <watcher url> <token>"}
	}
	server := serverView{Name: name, Version: srv.Report.Version, State: "last report " + ago(now, srv.Received)}
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

// appView is one app: running or not, its domains as seen from outside,
// its databases with the last copy and the last proven restore.
func (w *watcher) appView(server string, app report.App, now time.Time) appView {
	a := appView{Name: app.Name, State: "v" + strconv.Itoa(app.Release) + " running"}
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
func (w *watcher) domainLine(name string, running bool) lineView {
	d := w.state.Domains[name]
	switch {
	case !running || d == nil:
		return lineView{Name: name, State: "not visited"}
	case d.Visited.IsZero():
		return lineView{Name: name, State: "not visited yet"}
	case d.Down:
		return lineView{Name: name, State: "down: " + d.Reason, Problem: name + " is down: " + d.Reason + "."}
	}
	return lineView{Name: name, State: "up"}
}

// databaseLine says when the last copy was made and which copy a restore
// last proved. A failed check or a backup error makes it red.
func databaseLine(server, app string, db report.Database, now time.Time) lineView {
	line := lineView{Name: db.Name + " (" + db.Kind + ")", State: "copy " + ago(now, db.LastCopy) + ", restore proven " + ago(now, db.CheckedCopy)}
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
func problems(server serverView) []string {
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
func outageLine(o outage, now time.Time) outageView {
	what := o.Server + " stopped reporting"
	if o.Domain != "" {
		what = o.Domain + " down (" + o.App + " on " + o.Server + ")"
	}
	line := outageView{What: what, From: clock(o.From), Reason: o.Reason, Open: o.To.IsZero()}
	if line.Open {
		line.Lasted = "still, " + spoken(now.Sub(o.From))
	} else {
		line.Lasted = spoken(o.To.Sub(o.From))
	}
	return line
}

// checkLine is one restore check, newest first on the page.
func checkLine(c check) checkView {
	line := checkView{What: c.Database + " of " + c.App + " on " + c.Server, At: clock(c.At), OK: c.OK}
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

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="60">
<title>{{.Name}} watcher</title>
<style>
:root { color-scheme: light dark; --fg: #111; --bg: #fff; --muted: #777; --line: #e5e5e5; --bad: #c00; --good: #080; }
@media (prefers-color-scheme: dark) { :root { --fg: #eee; --bg: #111; --muted: #888; --line: #2a2a2a; --bad: #f55; --good: #5c5; } }
body { margin: 0 auto; max-width: 56rem; padding: 2rem 1rem; font: 15px/1.5 ui-sans-serif, system-ui, sans-serif; color: var(--fg); background: var(--bg); }
h1 { font-size: 1.25rem; margin: 0 0 1rem; }
h2 { font-size: 1rem; margin: 2.5rem 0 .5rem; }
table { width: 100%; border-collapse: collapse; }
td, th { text-align: left; padding: .4rem .5rem .4rem 0; border-top: 1px solid var(--line); vertical-align: top; }
th { font-weight: 600; color: var(--muted); font-size: .85rem; }
ul { padding-left: 1.2rem; margin: .25rem 0; }
.muted { color: var(--muted); }
.dot { display: inline-block; width: .55rem; height: .55rem; border-radius: 50%; margin-right: .4rem; background: var(--good); }
.dot.bad { background: var(--bad); }
.bad { color: var(--bad); }
.wrap { overflow-x: auto; }
</style>
</head>
<body>
<h1>{{.Name}} watcher</h1>
{{if .NeedsYou}}<div class="bad"><strong>Needs you</strong><ul>{{range .NeedsYou}}<li>{{.}}</li>{{end}}</ul></div>
{{else}}<p><span class="dot"></span>Everything is fine.</p>{{end}}
{{range .Servers}}
<h2><span class="dot{{if not .OK}} bad{{end}}"></span>{{.Name}} <span class="muted">{{.State}}{{if .Version}}, {{.Version}}{{end}}</span></h2>
{{if .Apps}}<div class="wrap"><table>
<tr><th>App</th><th>Domains, from outside</th><th>Databases</th></tr>
{{range .Apps}}<tr>
<td><span class="dot{{if not .OK}} bad{{end}}"></span>{{.Name}} <span class="muted">{{.State}}</span></td>
<td>{{range .Domains}}<div{{if .Problem}} class="bad"{{end}}>{{.Name}} <span class="muted">{{.State}}</span></div>{{else}}<span class="muted">none</span>{{end}}</td>
<td>{{range .Databases}}<div{{if .Problem}} class="bad"{{end}}>{{.Name}} <span class="muted">{{.State}}</span></div>{{else}}<span class="muted">none</span>{{end}}</td>
</tr>{{end}}
</table></div>{{else}}<p class="muted">No apps.</p>{{end}}
{{else}}
<p class="muted">No servers yet. Add one with <code>{{.Name}} watcher token &lt;server&gt; --url &lt;url&gt;</code>.</p>
{{end}}
<h2>Outages</h2>
{{if .Outages}}<div class="wrap"><table>
<tr><th>What</th><th>From</th><th>How long</th><th>Why</th></tr>
{{range .Outages}}<tr{{if .Open}} class="bad"{{end}}><td>{{.What}}</td><td>{{.From}}</td><td>{{.Lasted}}</td><td>{{.Reason}}</td></tr>{{end}}
</table></div>{{else}}<p class="muted">None.</p>{{end}}
<h2>Restore checks</h2>
{{if .Checks}}<div class="wrap"><table>
<tr><th>Database</th><th>Ran</th><th>Result</th></tr>
{{range .Checks}}<tr{{if not .OK}} class="bad"{{end}}><td>{{.What}}</td><td>{{.At}}</td><td>{{.Result}}</td></tr>{{end}}
</table></div>{{else}}<p class="muted">None yet.</p>{{end}}
</body>
</html>
`))
