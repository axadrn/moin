---
title: "Catalog"
description: "Open source apps on your own server in one click, with every version and a way back from each update"
group: "Apps"
order: 6
---

The catalog is the place with open source apps you install in one click, such as Umami, n8n, Open WebUI, Grafana, Nextcloud, Vaultwarden, Outline and Paperless-ngx. Pick one, a version and a server; a minute later it runs, its secrets generated and its database backed up like any app's.

## Install an app

```surfaces
Web: **Apps**, **New app**, **Open the catalog**, the app, **Install**; on the site, **Deploy with moin** on its page
CLI: moin catalog install umami --server web-1 [--version 3.4.1] [--name stats] [--domain stats.example.com]
TUI: opens the web: `w`, then `c`
API: POST /api/v1/catalog/{slug}/install
MCP: install_catalog_app
```

It creates the app, generates every secret as an encrypted env var, sets `VERSION` and deploys. No version takes the newest, no name the app's own, like `umami`. Add a [domain](/docs/guides/domains) now or later. Some apps have a first login, such as Umami's admin and `umami`; its page in the catalog says so under **First sign-in**, and `moin env --show` shows the generated passwords.

## See the catalog

The public catalog is at [/catalog](/catalog), one page per app: what it is, its website, source and license, its image on its registry, and its versions.

```surfaces
Web: **Apps**, **New app**, **Open the catalog**; or /catalog on the site
CLI: moin catalog, then moin catalog umami for its versions
TUI: opens the web: `w`, then `c`
API: GET /api/v1/catalog, GET /api/v1/catalog/{slug}
MCP: list_catalog, get_catalog_app
```

Pick a category, and the order: newest release first, or by name. Both stay in the address, so a link shows the same list. To find one app, use the search at the top right (`⌘K` or `/`): it finds an app by its name, its category or what it does, and a typo still finds it: `nfty` finds ntfy.

moin reads each app's image registry once an hour, so every version shows the hour it ships, newest first. Any of them can be installed, an older one too.

## Update an app

The app page says when a new version is out, such as "Umami 3.4.1 is out, released 2h ago". An update sets `VERSION` and deploys. moin takes a copy right before, so its [backups](/docs/guides/backups) offer "Before the update to 3.4.1".

```surfaces
Web: the app page, **Update** (or another version, **Deploy**)
CLI: moin update umami [--version 3.4.1]
TUI: on the app, `U`, then `y`
API: POST /api/v1/apps/{app}/update
MCP: update_app
```

An older version may not undo the database migrations of a newer one: go back to "Before the update to …" instead ([restore](/docs/guides/backups#go-back)).

## Add an app

Missing one? Open an [issue](https://github.com/axadrn/moin/issues), or send a pull request: one folder, `controlplane/content/catalog/<app>`, two files.

`app.md` says what the app is:

```markdown
---
name: Miniflux
pitch: A fast, quiet feed reader. No noise, no tracking.
category: Reading
website: https://miniflux.app
source: github.com/miniflux/v2
license: Apache-2.0
image: miniflux/miniflux
database: postgres
port: 8080
login: "Sign in as admin with the password in ADMIN_PASSWORD: moin env --show."
---

Two or three plain sentences: what it does, and where its data lives.
```

`compose.yml` runs it:

```yaml
x-moin:
  service: web
  port: 8080
  health: /healthcheck

services:
  web:
    image: miniflux/miniflux:${VERSION}
    environment:
      DATABASE_URL: postgres://miniflux:${DB_PASSWORD}@db/miniflux?sslmode=disable
    depends_on:
      db:
        condition: service_healthy
  db:
    image: postgres:17
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U miniflux -d miniflux"]
      interval: 5s
      timeout: 5s
      retries: 30
    environment:
      POSTGRES_USER: miniflux
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_DB: miniflux
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
```

The rules, which a test checks:

| Rule | |
| --- | --- |
| Image | on Docker Hub or `ghcr.io`, for `amd64` and `arm64`, run as `<image>:${VERSION}` |
| Secrets | every one a `${NAME}`, generated on install: 32 random bytes as URL-safe text; a name ending in `_HEX` gets 64 hex digits, `_HEX16` 32. Never write a password out |
| Its own domain | `${APP_URL}` is `https://<its domain>`, `${APP_DOMAIN}` the domain alone; an app that uses either needs a domain on install |
| Database | Postgres in the same compose file with a `pg_isready` health check the app waits for, or SQLite under `./data` |
| Versions | only tags like `1.2.3` or `v1.2.3` count, unless `versions:` sets a regular expression. A project that tags betas like versions sets `stable:` to its channel tag, such as `stable` |
| Category | AI, Analytics, Automation, Monitoring, Communication, Websites, Productivity, Documents, Reading, Developer tools, Security, Marketing, Finance or Media |

Install it once on a server of your own with `moin catalog install` before you open the pull request.

## Good to know

- The workspace needs a [bucket](/docs/guides/bucket): the app's compose file goes to the server through it, like a deploy from the CLI.
- A catalog app is an ordinary app: logs, metrics, uptime, domains, rollback and [backups](/docs/guides/backups) all work.
- A self-hosted control plane ships the same catalog and asks the registries itself; nothing goes through us. [How moin works](/docs/concepts/how-moin-works#the-catalog).
