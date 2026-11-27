package watcher

import (
	"crypto/subtle"
	"html/template"
	"net/http"
	"slices"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// page is the one page: every server, its apps, the last proven restore,
// recent outages. It reads; nothing on it changes anything. It asks for
// the password of the config, any user name.
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
	rw.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	if err := pageTemplate.Execute(rw, view); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
	}
}

type pageView struct {
	Name    string
	Servers []serverView
	Outages []outageView
	Checks  []checkView
}

type serverView struct {
	Name, Version, Seen, State string
	OK                         bool
	Apps                       []appView
}

type appView struct {
	Name    string
	Release int
	OK      bool
	State   string
	Domains []domainView
	DBs     []dbView
}

type domainView struct {
	Name, State string
	OK          bool
}

type dbView struct {
	Name, Kind, Copy, Proven, Error string
	OK                              bool
}

type outageView struct {
	What, From, Lasted, Reason string
	Open                       bool
}

type checkView struct {
	What, At, Copy, Error string
	OK                    bool
}

// view is what the page shows at now. The caller holds w.mu.
func (w *watcher) view(now time.Time) pageView {
	v := pageView{Name: build.Name}
	for _, name := range sortedKeys(w.config.Servers) {
		srv := w.state.Servers[name]
		if srv == nil {
			v.Servers = append(v.Servers, serverView{Name: name, State: "no report yet"})
			continue
		}
		sv := serverView{Name: name, Version: srv.Report.Version, Seen: ago(now, srv.Received), OK: !srv.Silent, State: "reports"}
		if srv.Silent {
			sv.State = "silent"
		}
		for _, app := range srv.Report.Apps {
			av := appView{Name: app.Name, Release: app.Release, OK: app.Running, State: "running"}
			if !app.Running {
				av.State = "stopped"
			}
			for _, d := range app.Domains {
				dv := domainView{Name: d, OK: true, State: "up"}
				if state := w.state.Domains[d]; state == nil || !app.Running {
					dv.State = "not visited"
				} else if state.Down {
					dv.OK, dv.State = false, "down: "+state.Reason
				}
				av.OK = av.OK && dv.OK
				av.Domains = append(av.Domains, dv)
			}
			for _, db := range app.Databases {
				dv := dbView{Name: db.Name, Kind: db.Kind, Copy: ago(now, db.LastCopy), Proven: ago(now, db.CheckedCopy), Error: db.Error}
				dv.OK = db.Error == "" && (db.LastCheck.IsZero() || db.CheckOK)
				av.OK = av.OK && dv.OK
				av.DBs = append(av.DBs, dv)
			}
			sv.OK = sv.OK && av.OK
			sv.Apps = append(sv.Apps, av)
		}
		v.Servers = append(v.Servers, sv)
	}
	for _, o := range slices.Backward(w.state.Outages) {
		what := o.Server + " stopped reporting"
		if o.Domain != "" {
			what = o.Domain + " down (" + o.Server + " · " + o.App + ")"
		}
		ov := outageView{What: what, From: clock(o.From), Reason: o.Reason, Open: o.To.IsZero()}
		if ov.Open {
			ov.Lasted = "still on, " + spoken(now.Sub(o.From))
		} else {
			ov.Lasted = spoken(o.To.Sub(o.From))
		}
		v.Outages = append(v.Outages, ov)
	}
	for _, c := range slices.Backward(w.state.Checks) {
		v.Checks = append(v.Checks, checkView{
			What: c.Server + " · " + c.App + " · " + c.Database, At: clock(c.At),
			Copy: clock(c.Copy), Error: c.Error, OK: c.OK,
		})
	}
	return v
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
h1 { font-size: 1.25rem; margin: 0 0 2rem; }
h2 { font-size: 1rem; margin: 2.5rem 0 .5rem; }
table { width: 100%; border-collapse: collapse; }
td, th { text-align: left; padding: .4rem .5rem .4rem 0; border-top: 1px solid var(--line); vertical-align: top; }
th { font-weight: 600; color: var(--muted); font-size: .85rem; }
.server { margin-bottom: 2rem; }
.server h2 { margin-top: 0; }
.muted { color: var(--muted); }
.dot { display: inline-block; width: .55rem; height: .55rem; border-radius: 50%; margin-right: .4rem; background: var(--good); }
.bad .dot, .dot.bad { background: var(--bad); }
.bad { color: var(--bad); }
.wrap { overflow-x: auto; }
</style>
</head>
<body>
<h1>{{.Name}} watcher</h1>
{{range .Servers}}
<section class="server">
<h2><span class="dot{{if not .OK}} bad{{end}}"></span>{{.Name}} <span class="muted">{{.State}}{{if .Seen}}, last report {{.Seen}}{{end}}{{if .Version}}, {{.Version}}{{end}}</span></h2>
{{if .Apps}}<div class="wrap"><table>
<tr><th>App</th><th>Domains</th><th>Databases: last copy, last proven restore</th></tr>
{{range .Apps}}<tr>
<td><span class="dot{{if not .OK}} bad{{end}}"></span>{{.Name}} <span class="muted">v{{.Release}} {{.State}}</span></td>
<td>{{range .Domains}}<div{{if not .OK}} class="bad"{{end}}>{{.Name}} <span class="muted">{{.State}}</span></div>{{else}}<span class="muted">none</span>{{end}}</td>
<td>{{range .DBs}}<div{{if not .OK}} class="bad"{{end}}>{{.Name}} <span class="muted">{{.Kind}}, copy {{.Copy}}, proven {{.Proven}}</span>{{if .Error}}<br>{{.Error}}{{end}}</div>{{else}}<span class="muted">none</span>{{end}}</td>
</tr>{{end}}
</table></div>{{else}}<p class="muted">No apps.</p>{{end}}
</section>
{{else}}
<p class="muted">No servers yet. Add one with <code>{{.Name}} watcher add &lt;server&gt;</code>.</p>
{{end}}
<h2>Outages</h2>
{{if .Outages}}<div class="wrap"><table>
<tr><th>What</th><th>From</th><th>How long</th><th>Why</th></tr>
{{range .Outages}}<tr{{if .Open}} class="bad"{{end}}><td>{{.What}}</td><td>{{.From}}</td><td>{{.Lasted}}</td><td>{{.Reason}}</td></tr>{{end}}
</table></div>{{else}}<p class="muted">None.</p>{{end}}
<h2>Restore checks</h2>
{{if .Checks}}<div class="wrap"><table>
<tr><th>Database</th><th>Ran</th><th>Copy</th><th>Result</th></tr>
{{range .Checks}}<tr{{if not .OK}} class="bad"{{end}}><td>{{.What}}</td><td>{{.At}}</td><td>{{if .OK}}{{.Copy}}{{end}}</td><td>{{if .OK}}restored{{else}}failed: {{.Error}}{{end}}</td></tr>{{end}}
</table></div>{{else}}<p class="muted">None yet.</p>{{end}}
</body>
</html>
`))
