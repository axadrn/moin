# AGENTS.md

This is the website at moin.run: the landing page, the docs, the blog, the
changelog, the catalog, the comparisons and the legal pages. It is moin
cloud too: sign-in by GitHub or a mail link, one workspace per mail, a
watcher per workspace that servers report to, billing through Stripe,
the copies a server can add to but never delete, and the API of
`moin cloud` in the CLI. How moin works is in `content/docs`, the only
source of truth.

## Rules

- Native, idiomatic, very simple Go. Standard library first.
- Every page is a full page load. htmx only answers the search palette.
- No foreign UI library, not even for icons. The look is
  `assets/css/site.css` and `internal/ui/components`; icons are SVG files in
  `assets/icons`, drawn on one 24 grid with `currentColor`. Brand marks keep
  their official shapes.
- Never edit generated `*_templ.go` files. Edit `.templ` files; `task dev`
  regenerates them, or `go tool templ generate`.
- Keep secrets out of templates. Request context holds only
  `config.RenderConfig`, the public values templates need.
- moin cloud never reaches a server. It receives reports and copies, and
  answers a daemon only with a status code, or for a copy with one upload
  address. Nothing it says makes a server do anything else.
- It never sees data: a copy arrives encrypted for keys it does not hold.
  It knows only what a report holds, and the size of each copy.

## Code map

- `cmd/server`: the site's `main`.
- `cmd/import`: brings the paying workspaces of the old service into moin
  cloud, once.
- `cmd/devport`: finds a free port for `task dev`.
- `internal/routes`: which handler answers which address, and the
  middleware around all of them.
- `internal/handler`: reads the request, renders the page.
- `internal/service`: reads the texts of `content/`: docs, blog,
  changelog, roadmap, legal pages, sitemap.
- `internal/db`: opens the SQLite file of moin cloud and runs the
  migrations in `internal/db/migrations`.
- `internal/workspace`: workspaces, sessions, logins and servers in the
  database.
- `internal/watchers`: one watcher of `internal/watcher` per workspace,
  fed by `POST /report`. Alerts go out by mail, during the 30-day trial
  that the first report starts, and while the workspace pays.
- `internal/copies`: the copies a server can't delete. An upload address
  for 15 minutes, a Cloudflare R2 bucket with a 14-day lock, 5 GB per
  workspace in the trial, 50 GB paid or in the free year, gone 30 days
  after a trial or a subscription ends.
- `internal/stripe`: the little of Stripe that billing needs: checkout,
  the customer portal, the subscription, the webhooks.
- `internal/counts`: the sums over everyone the landing page shows. No
  number about one customer leaves it.
- `internal/mailer`: mail through Cloudflare Email Sending, or to the log.
- `internal/github`: sign-in with the GitHub App.
- `internal/githubrepo`: what GitHub says about the repo: its stars and
  downloads.
- `internal/middleware`: what runs around every request.
- `internal/catalog`: reads the apps of `catalog/` at the root of the repo.
- `internal/compare`: the comparisons with other tools.
- `internal/ui`: `components/` (the parts every page is built from),
  `layouts/` (what surrounds every page) and `pages/` (one file per page).
- `content/`: the texts, built into the binary, read from disk in
  development.
- `assets/`: styles, scripts, fonts, icons and images, served at
  `/assets/`.
- `deploy/`: the compose files and `update.sh` the Deploy workflow puts on
  the server of the site.

## Commands

- `task dev` runs the site with hot reload on a free port.
- `go test ./...` runs the tests; `go vet ./...` the checks.
