---
title: "Catalog"
description: "Open source apps on your own server in one click, every release, updates with a snapshot first"
order: 3
---

The catalog is a list of open source apps ops installs in one click, such as Umami, n8n, Grafana, Nextcloud, Vaultwarden and Paperless-ngx. Pick one, a version and a server. A minute later it runs, with its database backed up, every change.

- **Every release.** ops reads the image registry of each app once an hour. You see every release, the newest first, the hour it ships. Pick any of them, also an older one.
- **Its database is safe.** A Postgres or SQLite inside the app is backed up like any app's: every change in your bucket, a full copy before every deploy, and a restore check every day. See [Backups](/docs/guides/backups).
- **Secrets made for you.** Every password and key the app needs is generated on install, stored encrypted as an env var, never written in a file.
- **An ordinary app.** It runs on your server like an app you deploy yourself: logs, metrics, uptime, domains, rollback, all of it.

## See the catalog

The public catalog is at [/apps](/apps), one page per app.

```surfaces
Web: Apps, From the catalog; or /apps on the site
CLI: ops catalog, then ops catalog umami for its releases
TUI: :catalog, then :catalog umami
API: GET /api/v1/catalog, GET /api/v1/catalog/{slug}
MCP: list_catalog, get_catalog_app
```

## Install an app

Creates the app on the server, generates its secrets, sets `VERSION` and deploys it. The workspace needs a [bucket](/docs/guides/bucket): the app's compose file goes to the server through it, like a deploy from the CLI.

```surfaces
Web: Apps, From the catalog, the app, Install
CLI: ops catalog install umami --server web-1 [--version 3.4.1] [--name stats] [--domain stats.example.com]
TUI: :catalog install umami web-1 [3.4.1]
API: POST /api/v1/catalog/{slug}/install
MCP: install_catalog_app
```

No version takes the newest release, no name the app's own, like `umami`. Give it a domain then or later on the app page: [Domains](/docs/guides/domains). Some apps have a first login, such as Umami's admin and `umami`; the catalog page of the app says so. Generated passwords show with `ops env --show`.

## Update an app

The app page shows when a newer release is out, such as "Umami 3.4.1 is out, released 2h ago". An update sets `VERSION` and deploys. The deploy takes a snapshot of the app's database first, as every deploy does, so a release that breaks something is a [restore](/docs/guides/backups) away.

```surfaces
Web: the app page, the catalog panel, Update
CLI: ops update umami [--version 3.4.1]
TUI: on the app, :update [version]
API: POST /api/v1/apps/{app}/update
MCP: update_app
```

You can pick an older version too. Its app may not undo the database migrations of a newer one: the snapshot before the deploy is the way back.

## How it works

Each app is a folder in the ops repo, `controlplane/content/catalog/<app>`: an `app.md` with what it is, and a `compose.yml` that runs the image with `${VERSION}` as its tag and every secret as `${NAME}`. A self-hosted control plane ships the same catalog and asks the registries itself; nothing goes through us.

## Add an app

Missing one? Open an [issue](https://github.com/axadrn/ops/issues) and we add it, or add it yourself with a pull request: one folder, two files.

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
login: "Sign in as admin with the password in ADMIN_PASSWORD: ops env --show."
---

Two or three plain sentences: what it does, and where its data lives.
```

`compose.yml` runs it:

```yaml
x-ops:
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

- **The image** is on Docker Hub or `ghcr.io`, for `amd64` and `arm64`, and `compose.yml` runs it as `<image>:${VERSION}`.
- **Every secret** is a `${NAME}`: ops generates it on install. Never write a password out.
- **A database** is Postgres in the same compose file, with a `pg_isready` health check the app waits for, or SQLite under `./data`, so ops backs it up.
- **Versions**: by default only tags like `1.2.3` or `v1.2.3` count. A project with other tags sets `versions:` to a regular expression. One that tags its betas like releases sets `stable:` to its channel tag, such as `stable`: newer releases stay out.
- **Category** is one of Analytics, Automation, Monitoring, Communication, Productivity, Documents, Reading, Developer tools, Security, Marketing, Finance or Media.

Install it once on a server of your own with `ops catalog install` before you open the pull request.
