package watcher

import (
	"crypto/subtle"
	"html/template"
	"net/http"

	"github.com/axadrn/moin/internal/build"
)

// page is the one page of `moin watcher run`: the watcher's View. It
// only reads: nothing on it changes anything. It asks for the password
// of the config, any user name.
func (s *site) page(rw http.ResponseWriter, r *http.Request) {
	_, password, ok := r.BasicAuth()
	if !ok || subtle.ConstantTimeCompare([]byte(password), []byte(s.config.Password)) != 1 {
		rw.Header().Set("WWW-Authenticate", `Basic realm="`+build.Name+` watcher", charset="UTF-8"`)
		http.Error(rw, "the watcher asks for its password", http.StatusUnauthorized)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	if err := pageTemplate.Execute(rw, s.watcher.View()); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
	}
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
