# AGENTS.md

This is the website at moin.run: the landing page, the docs, the blog, the
changelog, the catalog, the comparisons and the legal pages. It has no
database, no sign-in and no API. How moin works is in `content/docs`, the
only source of truth.

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

## Code map

- `cmd/server`: the site's `main`.
- `internal/routes`: which handler answers which address, and the
  middleware around all of them.
- `internal/handler`: reads the request, renders the page.
- `internal/service`: reads the texts of `content/`: docs, blog,
  changelog, roadmap, legal pages, sitemap.
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
