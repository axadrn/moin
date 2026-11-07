---
title: "Backups"
description: "Copies on your schedule, go back to one, undo it, and download a copy"
group: "Servers and data"
order: 3
---

moin copies your database into your bucket on a schedule you pick, and keeps the copies as long as you say. With SQLite it can also write down every change, so you go back to any minute. Every day moin checks that a copy restores.

It backs up every SQLite and Postgres database of your apps once the workspace has a [bucket](/docs/guides/bucket#add-it): hourly, each copy kept 7 days, until you change it. Your data goes from your server to your own bucket only, never through moin. [How backups work](/docs/concepts/backups) says how.

```
your server ──copies──▶ your bucket
     ▲                      │
     └──── a restore ◀──────┘

hub: how often, how
long, the list. Never your data.
```

## See your backups

One line says how each database stands: "Last copy 12m ago · checked 2h ago", or in red why it does not work, like "The bucket can't be reached.", with the details under it. Then the moments you can go back to.

```surfaces
Web: the app, **Data**, a database
CLI: moin backups my-app
TUI: on the app, `b`
API: GET /api/v1/apps/{app}/backups
MCP: get_backups
```

## How often and how long

Two settings per app, for all of its databases:

- **How often**: every 15 minutes, hourly or daily. Hourly unless you change it.
- **Keep**: 1, 3, 7 or 30 days. 7 unless you change it. Older copies go; the newest one always stays.
- **Every change** (SQLite only): on unless you turn it off. It writes down every change between the copies, so you can go back to any minute of the days you keep. Turning it off removes what it wrote down; the copies stay.

```surfaces
Web: the app, **Data**, a database: **How often**, **Keep**, **Every change**
CLI: moin backups my-app --every 15m --keep 30d --every-change on
TUI: on the app, `b`; the settings open on the web
API: PUT /api/v1/apps/{app}/backups
MCP: set_backups
```

`--every` takes `15m`, `1h` or `1d`, `--keep` takes `1d`, `3d`, `7d` or `30d`, `--every-change` takes `on` or `off`; set one or all of them. The API and MCP take `every_minutes` (15, 60 or 1440), `keep_days` (1, 3, 7 or 30) and `every_change`. The daemon takes the new settings at once; the next copy comes when the newest one is older than **How often**.

## The copies

```
SQLite: app.db
  ─▶ temp copy (VACUUM INTO)
  ─▶ gzip ─▶ your bucket
  ─▶ temp copy deleted at once

Postgres: pg_dump
  ─▶ straight into your bucket
  (nothing waits on the disk)
```

`VACUUM INTO` is SQLite's own way to make a clean copy while your app keeps running; `pg_dump` is the same for Postgres. The SQLite copy is a file on your server for a moment, because `VACUUM INTO` writes into a file; moin uploads it and throws it away. `pg_dump` writes a stream, and moin sends it straight to your bucket.

Besides the schedule, moin takes one extra copy right before every deploy, every [catalog](/docs/guides/catalog) update and every [data](/docs/guides/data) edit you make in moin. They show by name:

- **At 14:00**: a copy from your schedule. One from another day says it: **Nov 6 at 14:00**
- **Before v12**: right before a deploy started v12
- **Before the update to 3.4.1**: right before a catalog update
- **Before your SQL change**, **Before your edit in users**, **Before the row added to users**: right before a data edit
- **1 minute ago**, **15 minutes ago**, **1 hour ago**, **Yesterday this time**: SQLite with Every change
- **Before the restore at 14:05**: the database as it was before a restore; going back to it undoes the restore

Times are in UTC.

## Go back

Pick a moment, then **Restore**, and type the app's name. moin builds the database beside the live one while your app runs, stops the app for one quick switch, and starts it again, also when something fails on the way. A big database takes a while: the restore runs on its own, the page says "Restoring" and then how it ended, and the CLI and the TUI wait for it.

```
build beside ─▶ switch ─▶ start
                  │
       the old one stays: undo
```

```surfaces
Web: the app, **Data**, a database: pick a moment under **Go back to**, **Restore**, then type the app name
CLI: moin restore my-app --to before-v12
TUI: on the app, `b`, pick a moment, `enter`, then type the app name
API: POST /api/v1/apps/{app}/restores
MCP: restore_app
```

`--to` takes a moment from `moin backups`, by its ID or its label (`1h`, `before-v12`, `20261107T140000Z`, `"Before your SQL change"`), or a time in UTC to the minute: `14:01` for today, `"2026-11-07 14:01"` or an RFC 3339 time. SQLite with Every change goes to that minute; otherwise moin takes the newest copy at or before it. In the TUI `t` asks for a time. The API and MCP take `moment` or `at`; the API answers `202` with the restore to follow at `GET /api/v1/apps/{app}/restores/{restore}`, and `restore_app` answers its state, then takes its `id` to tell how it stands. Two moments with the same label, like two SQL changes, need the moment's ID. An app with more than one database: name it with `--db /data/app.db` or `--db postgres-db`.

## Undo a restore

Every restore keeps the database as it was right before, as long as you keep copies. Go back to **Before the restore at 14:05** to undo it, the same way:

```surfaces
Web: the app, **Data**, a database: **Before the restore at 14:05**, **Restore**
CLI: moin restore my-app --to undo-20261107T140500Z
TUI: on the app, `b`, pick **Before the restore at …**, `enter`
API: POST /api/v1/apps/{app}/restores
MCP: restore_app
```

An undo is a restore too: it keeps the database before it, so you can undo the undo.

## Download a copy

A link to a copy in your bucket: the newest one, or the one you name. The link works for 15 minutes, and the file comes straight from your bucket, not through moin.

```surfaces
Web: the app, **Data**, a database, **Download a copy**
CLI: moin backup download my-app
TUI: on the app, `b`, then `c`
API: POST /api/v1/apps/{app}/backups/download
MCP: download_backup
```

The CLI saves it in the current folder; `--link` prints the link instead, `--to before-v12` takes another copy than the newest. A SQLite copy is gzipped: `gunzip` it and open it with `sqlite3`. A Postgres copy is a `pg_dump`: `pg_restore --list` shows what is in it, `pg_restore` puts it into any Postgres. You need no moin for either. Right after the first deploy with a bucket there is no copy yet: the first one comes within a minute.

## Postgres

Any service that runs Postgres is backed up: `postgres`, `postgis/postgis`, `pgvector/pgvector` or `timescale/timescaledb`, Debian or Alpine. No password, no change to your `compose.yml`:

```yaml
services:
  web:
    build: .
    environment:
      DATABASE_URL: postgres://app:${POSTGRES_PASSWORD}@db:5432/app
  db:
    image: postgres:17
    environment:
      POSTGRES_USER: app
      POSTGRES_DB: app
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - pgdata:/var/lib/postgresql/data
volumes:
  pgdata:
```

In moin this database is `postgres-db` (`postgres-<service>`): in the bucket, in the app's backups and in `--db postgres-db`. That is the database `POSTGRES_DB` names (the user's name when it is not set).

moin backs up every database of the service, not only that one. One you made yourself, like `analytics`, is `postgres-db/analytics`: its own copies, its own **Go back to** list on the page of `postgres-db`, `--db postgres-db/analytics` in the CLI. Each one restores on its own; the others stay as they are.

## Requirements

- **SQLite**: the file is on a volume. Every change needs WAL mode (`PRAGMA journal_mode=WAL`), as most apps use. Do not run a second Litestream for the same file.
- **Postgres**: a service that runs one of the images above. Keep its data on a volume, so it survives a restart.
- No database shows up? See [Data](/docs/guides/data#requirements).

## Good to know

- A restore replaces the live data, so moin asks you to type the app's name first, on the web, in the CLI and in the TUI. The hub checks the name, not only the screen: scripts pass it with `--confirm <app>` (`--yes` is not enough), the API and MCP take it as `confirm`, and a wrong one answers `confirm_mismatch`.
- A restore needs room on the server's disk for one more database, the one it builds beside the live one. The old one stays as the undo until the copies of that day go.
- After a restore of a SQLite file, Every change starts again from the restored file. To go back to before the restore, use **Before the restore at …**.
- A restore goes on when the server goes offline or the hub restarts in the middle: it shows as "the server is offline", no other restore of the app starts meanwhile, and moin asks the daemon every minute how it ended once the server is back. The CLI and the TUI wait 10 minutes for that, then say what to check. A daemon that stops in the middle (a reboot) ends the restore when it starts again: the app runs again, on the database before or the restored one, never on half of one, and the restore says which.
- A restore that fails says why. Mostly nothing changed then, and the app runs again; when only the start after the switch failed, it says so: the restored data is in place, and starting the app again is what is left.
- When an app moves to another bucket, its copies start again there. What is in the old bucket stays.
- Removing the bucket from moin stops its backups; what is in it stays. See [Remove a bucket](/docs/guides/bucket#remove-a-bucket).
- Removing an app keeps its copies in your bucket until you delete them.
- To keep a server's backups in one bucket only, use the `backups-to` [rule](/docs/guides/security#rules).
- A daemon older than this version answers "update the daemon": update it under **Servers** or with `moin servers update`.
- Not sure which database to pick for a new app? [SQLite or Postgres?](/docs/guides/sqlite-or-postgres)
