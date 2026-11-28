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

// pageTemplate is the page in the look of moin cloud, as far as one file
// with no script and no font goes: a bar, what needs you, the servers as
// cards, the outages and restore checks as compact rows.
var pageTemplate = template.Must(template.New("page").Funcs(template.FuncMap{
	"red": func(s ServerView) bool { return s.Problem != "" },
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="60">
<title>{{.Name}} watcher</title>
<style>
:root { color-scheme: light dark; --bg: #f2f2f7; --card: #fff; --fg: #000; --muted: #6e6e73; --chip: #e5e5ea; --line: rgb(0 0 0 / 8%); --live: #1fa855; --failed: #e5383b; --failed-text: #b4262a; }
@media (prefers-color-scheme: dark) { :root { --bg: #000; --card: #1c1c1e; --fg: #fff; --muted: #98989f; --chip: #2c2c2e; --line: rgb(255 255 255 / 16%); --live: #34c76a; --failed: #ff5a5f; --failed-text: #ff8a8d; } }
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, sans-serif; -webkit-font-smoothing: antialiased; }
.page { max-width: 64rem; margin: 0 auto; padding: 0 16px; }
@media (min-width: 640px) { .page { padding: 0 24px; } }
header { border-bottom: 1px solid var(--line); }
header .page { display: flex; align-items: center; gap: 12px; height: 64px; }
.mark { font-size: 20px; font-weight: 800; letter-spacing: -0.04em; }
.slash, .muted { color: var(--muted); }
.pill { padding: 2px 8px; border-radius: 999px; background: var(--chip); color: var(--muted); font-size: 12px; font-weight: 500; }
main { display: flex; flex-direction: column; gap: 16px; padding-top: 32px; padding-bottom: 64px; }
h1 { margin: 0; font-size: 30px; line-height: 36px; font-weight: 600; letter-spacing: -0.025em; }
h2 { margin: 0; font-size: 16px; font-weight: 500; }
p { margin: 0; }
.card { display: flex; flex-direction: column; gap: 16px; min-width: 0; padding: 24px; border-radius: 16px; background: var(--card); }
.notice { display: flex; gap: 12px; padding: 16px 20px; border-radius: 16px; background: var(--card); box-shadow: inset 0 0 0 1px color-mix(in oklab, var(--failed) 45%, transparent); }
.notice div { display: flex; flex-direction: column; gap: 4px; overflow-wrap: anywhere; }
.dot { display: inline-block; flex: none; width: 8px; height: 8px; border-radius: 50%; background: var(--live); }
.dot.bad { background: var(--failed); }
.dot.off { background: none; border: 1.5px solid var(--muted); }
.notice > .dot { margin-top: 6px; }
.status { display: inline-flex; align-items: center; gap: 8px; white-space: nowrap; }
.bad-text { color: var(--failed-text); }
.grid { display: grid; gap: 16px; grid-template-columns: minmax(0, 1fr); }
.head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.name { font-size: 16px; font-weight: 500; }
.small { font-size: 12px; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { display: inline-flex; align-items: center; gap: 8px; height: 32px; padding: 0 12px; border-radius: 12px; background: var(--chip); font-weight: 500; }
.chip span { font-weight: 400; color: var(--muted); }
.rows { margin: 0 -24px -24px; }
.row { display: grid; grid-template-columns: minmax(0, 1.4fr) repeat(2, minmax(0, 1fr)); gap: 0 16px; align-items: center; min-height: 52px; padding: 8px 24px; border-top: 1px solid var(--line); overflow-wrap: anywhere; }
.row.th { min-height: 36px; border-top: 0; color: var(--muted); font-size: 12px; }
.row > :first-child { font-weight: 500; }
.lines { display: flex; flex-direction: column; gap: 4px; }
.row .status { white-space: normal; }
@media (max-width: 639px) { .row { display: flex; flex-wrap: wrap; gap: 4px 12px; color: var(--muted); } .row > :first-child { flex-basis: 100%; color: var(--fg); } .row.th { display: none; } }
</style>
</head>
<body>
<header><div class="page"><span class="mark">{{.Name}}.</span><span class="slash">/</span><b>watcher</b><span class="pill">self-hosted</span></div></header>
<main class="page">
<h1>{{len .Servers}} {{if eq (len .Servers) 1}}server{{else}}servers{{end}}</h1>
<p class="muted">This watcher reads reports and sends alerts. Nothing here changes a server.</p>
{{if .NeedsYou}}<div class="notice" role="status"><i class="dot bad"></i><div><b>Needs you</b>{{range .NeedsYou}}<span>{{.}}</span>{{end}}</div></div>
{{else if .Servers}}<p class="status"><i class="dot"></i>Everything is fine.</p>{{end}}
{{if .Servers}}<div class="grid">
{{range .Servers}}<section class="card">
<div class="head"><div><div class="name">{{.Name}}</div><div class="muted small">{{if .Version}}{{.Version}} · {{end}}{{.State}}</div></div>
<span class="status{{if red .}} bad-text{{end}}"><i class="dot{{if red .}} bad{{else if not .Version}} off{{end}}"></i>{{if red .}}silent{{else if .Version}}online{{else}}no report yet{{end}}</span></div>
{{if .Apps}}<div class="chips">{{range .Apps}}<span class="chip"><i class="dot{{if not .OK}} bad{{else if .Stopped}} off{{end}}"></i>{{.Name}} <span>{{.State}}</span></span>{{end}}</div>
<div class="rows"><div class="row th"><span>App</span><span>Domains, from outside</span><span>Databases</span></div>
{{range .Apps}}<div class="row"><span>{{.Name}}</span>
<span class="lines">{{range .Domains}}<span{{if .Problem}} class="bad-text"{{end}}>{{.Name}} <span class="muted">{{.State}}</span></span>{{else}}<span class="muted">none</span>{{end}}</span>
<span class="lines">{{range .Databases}}<span{{if .Problem}} class="bad-text"{{end}}>{{.Name}} <span class="muted">{{.State}}</span></span>{{else}}<span class="muted">none</span>{{end}}</span></div>{{end}}</div>
{{else}}<p class="muted">No apps.</p>{{end}}
</section>{{end}}
</div>
{{else}}<section class="card"><h2>No servers yet</h2><p class="muted">Add one on this watcher with <code>{{.Name}} watcher token &lt;server&gt;</code>, then run the line it prints on your laptop.</p></section>{{end}}
<section class="card"><h2>Alerts</h2>
{{if .Outages}}<div class="rows"><div class="row th"><span>What</span><span>From</span><span>How long</span></div>
{{range .Outages}}<div class="row"><span class="status{{if .Open}} bad-text{{end}}"><i class="dot{{if .Open}} bad{{else}} off{{end}}"></i>{{.What}}</span><span>{{.From}}</span><span>{{.Lasted}}{{if .Reason}}: {{.Reason}}{{end}}</span></div>{{end}}</div>
{{else}}<p class="muted">None. An alert goes out when a server goes silent or a domain is down from outside.</p>{{end}}
</section>
<section class="card"><h2>Restore checks</h2>
{{if .Checks}}<div class="rows"><div class="row th"><span>Database</span><span>Ran</span><span>Result</span></div>
{{range .Checks}}<div class="row"><span class="status{{if not .OK}} bad-text{{end}}"><i class="dot{{if not .OK}} bad{{end}}"></i>{{.What}}</span><span>{{.At}}</span><span>{{.Result}}</span></div>{{end}}</div>
{{else}}<p class="muted">None yet. Each server restores the newest copy of every database once a day.</p>{{end}}
</section>
</main>
</body>
</html>
`))
