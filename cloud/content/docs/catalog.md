---
title: "Catalog"
description: "Open source apps like Umami and n8n and Vaultwarden on your server with one command. With backups and the daily restore check."
group: "Apps"
order: 2
---

The catalog holds open source apps, ready to run. Umami, n8n, Open WebUI, Grafana, Nextcloud, Vaultwarden, Outline, Paperless-ngx and more.

## See what is in it

```bash
moin catalog
```

Or browse it on the site: [/catalog](/catalog), one page per app.

## Deploy one

```bash
moin deploy my-server --catalog umami
```

You need no folder and no code. The app is called `umami`. For a second one, or another name, add `--app stats`.

Give it its domain right away:

```bash
moin deploy my-server --catalog umami --domain stats.example.com
```

The app knows its own address from the first start: in `APP_URL` and `APP_DOMAIN`. Without a domain that is its instant address. Add one later with `moin domains add`, and the address follows it, unless you set it yourself.

- The server makes every secret the app needs, like its database password. They never leave the server.
- Some apps have a first sign-in, such as an admin password. The app's page in the catalog says how.
- Its compose file comes with moin itself, so it is signed with each release.

## Like any app

A catalog app is an ordinary app. Logs, env vars, domains, rollback, backups and the daily restore check all work the same.

```bash
moin domains add umami stats.example.com
moin logs umami
```

## Update one

A catalog app runs the version in its env var `VERSION`, `latest` at first. `moin apps` shows it.

```bash
moin update umami
moin update umami --version 2.15.1
```

`moin update umami` fetches the newest images of that version and starts them, after a copy of the databases. `--version` sets a new one first. If the update fails, the version stays as it was and the old release runs again. Only you update it: moin never does it by itself.

## Add an app

Missing one? Open an [issue](https://github.com/axadrn/moin/issues), or send a pull request. An app is one folder, `catalog/<app>`, with two files: `app.md` says what the app is, `compose.yml` runs it. A test checks the rules. Look at any folder in [catalog/](https://github.com/axadrn/moin/tree/main/catalog) to start.

Its image must run on `amd64` and `arm64`. Its database is Postgres in the same compose file, or SQLite under `./data`, so moin can back it up.
