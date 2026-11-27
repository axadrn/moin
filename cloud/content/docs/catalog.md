---
title: "Catalog"
description: "Open source apps like Umami, n8n and Vaultwarden on your server with one command. With backups and the daily restore check."
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
moin deploy prod --catalog umami
```

You need no folder and no code. The app is called `umami`. For a second one, or another name, add `--app stats`.

- The server makes every secret the app needs, like its database password. They never leave the server.
- Some apps have a first sign-in, such as an admin password. The app's page in the catalog says how.
- Its compose file comes with moin itself, so it is signed with each release.

## Like any app

A catalog app is an ordinary app. Logs, env vars, domains, rollback, backups and the daily restore check all work the same.

```bash
moin domains add umami stats.example.com
moin logs umami
```

## Add an app

Missing one? Open an [issue](https://github.com/axadrn/moin/issues), or send a pull request. An app is one folder, `catalog/<app>`, with two files: `app.md` says what the app is, `compose.yml` runs it. A test checks the rules. Look at any folder in [catalog/](https://github.com/axadrn/moin/tree/main/catalog) to start.

Its image must run on `amd64` and `arm64`. Its database is Postgres in the same compose file, or SQLite under `./data`, so moin can back it up.
