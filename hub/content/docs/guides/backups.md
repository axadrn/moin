---
title: "Backups"
description: "Every change of SQLite, copies of Postgres, go back, undo it, and download"
group: "Servers and data"
order: 3
---

moin keeps your database safe in your own bucket. Every day it checks that it restores.

| | How it is kept | How far back | Checked |
| --- | --- | --- | --- |
| **SQLite** | every change is written down | back to any minute | every day |
| **Postgres** | a copy on your schedule and before every deploy | back to any copy | every day |

```
your server ──backups──▶ your bucket
     ▲                       │
     └──── a restore ◀───────┘

hub: how long, how often,
the list. Never your data.
```

## What moin backs up

moin backs up databases, not files.

It backs up every SQLite and Postgres database of your apps, once the workspace has a [bucket](/docs/guides/bucket#add-it).

Every hour moin looks for new databases. So a SQLite file your app makes long after the deploy gets backed up too.

What looks like a database but cannot be backed up shows in red: MySQL, MongoDB, or Postgres in an image moin does not know.

## See your backups

One line says how each database stands. SQLite: "Every change written down · checked 2h ago". Postgres: "Last copy 12m ago · checked 2h ago". Or, in red, why it does not work. Then the moments you can go back to.

```surfaces
Web: the app, **Data**, a database
CLI: moin backups my-app
TUI: on the app, `b`
API: GET /api/v1/apps/{app}/backups
MCP: get_backups
```

## How long and how often

Settings per app:

- **Keep**: 1, 3, 7 or 30 days. 7 unless you change it. It counts for SQLite and Postgres. The newest copy of Postgres always stays.
- **How often** (Postgres only): every 15 minutes, hourly or daily. Hourly unless you change it.

SQLite has nothing else to set. Every change is written down, always.

```surfaces
Web: the app, **Data**, a database: **Keep**, and **How often** for Postgres
CLI: moin backups my-app --keep 30d --every 15m
TUI: on the app, `b`; the settings open on the web
API: PUT /api/v1/apps/{app}/backups
MCP: set_backups
```

The CLI takes `15m`, `1h` or `1d`, and `1d`, `3d`, `7d` or `30d`. The API and MCP take `every_minutes` (15, 60 or 1440) and `keep_days` (1, 3, 7 or 30).

The schedule can also live in your repo:

```yaml
x-moin:
  backups:
    every: 15m
    keep: 30d
```

Then the file decides. moin shows the schedule locked, marked **from compose.yml**. Changing it elsewhere answers `schedule_in_file`. [Who decides](/docs/guides/config#who-decides).

## The moments

You go back to a moment. A moment has a name:

- **1 minute ago**, **15 minutes ago**, **1 hour ago**, **Yesterday this time**: SQLite
- **At 14:00**: a copy of Postgres. From another day: **Nov 6 at 14:00**
- **Before v12**: right before a deploy started v12
- **Before the update to 3.4.1**: right before a catalog update
- **Before your SQL change**, **Before your edit in users**, **Before the row added to users**: right before a data edit
- **Before the restore at 14:05**: going back to it undoes the restore

SQLite remembers these times: every change is already written down. Postgres takes a copy right before each deploy, catalog update and data edit. A Postgres database over 1 GB gets no copy before a data edit.

Times are in UTC.

## Go back

Pick a moment, press **Restore**, and type the app's name. moin builds the database beside the live one while your app runs. Then it stops the app for one quick switch and starts it again.

A big database takes a while. The page says "Restoring", then how it ended. The CLI and the TUI wait for it.

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

`--to` takes a moment from `moin backups`, by its ID or label: `1h`, `before-v12`, `20261107T140000Z`. Or a time in UTC: `14:01` for today, or `"2026-11-07 14:01"`. SQLite goes to that minute. Postgres takes the newest copy at or before it.

The API and MCP take `moment` or `at`. Two moments with the same label need the moment's ID. With more than one database, name one: `--db /data/app.db` or `--db postgres-db`.

## Undo a restore

Every restore keeps the database as it was right before, as long as the app keeps its backups. Go back to **Before the restore at 14:05** to undo it. An undo is a restore too, so you can undo the undo.

```surfaces
Web: the app, **Data**, a database: **Before the restore at 14:05**, **Restore**
CLI: moin restore my-app --to undo-20261107T140500Z
TUI: on the app, `b`, pick **Before the restore at …**, `enter`
API: POST /api/v1/apps/{app}/restores
MCP: restore_app
```

## Download a copy

A link to the newest state of your database in your bucket. It works for 15 minutes. The file comes straight from your bucket, not through moin.

- **SQLite**: moin restores the newest state, gzips it and puts it in your bucket. Open it with `gunzip`, then `sqlite3`.
- **Postgres**: the newest copy, a `pg_dump`. `pg_restore` puts it into any Postgres.

You need no moin for either. moin deletes SQLite downloads from your bucket after a day.

```surfaces
Web: the app, **Data**, a database, **Download a copy**
CLI: moin backup download my-app
TUI: on the app, `b`, then `c`
API: POST /api/v1/apps/{app}/backups/download
MCP: download_backup
```

The CLI saves it in the current folder. `--link` prints the link instead. For Postgres, `--to before-v12` takes another copy.

## Postgres

Any service that runs a Postgres image moin knows is backed up. No password, no change to your `compose.yml`:

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

In moin this database is `postgres-db` (`postgres-<service>`).

moin backs up every database of the service. One you made yourself, like `analytics`, is `postgres-db/analytics`. It has its own copies and its own **Go back to** list. Each one restores on its own.

## Requirements

- **SQLite**: the file is in a mounted folder or volume, in WAL mode (`PRAGMA journal_mode=WAL`). Do not run a second Litestream for the same file.
- **Postgres**: a service that runs one of the [known images](/docs/guides/data#requirements). Keep its data on a volume.

## Good to know

- A restore replaces the live data, so you type the app's name first. The hub checks it. Scripts pass it with `--confirm <app>`; `--yes` is not enough.
- A restore checks first that the disk has room.
- A restore goes on when the server goes offline or the hub restarts. The CLI and the TUI wait 10 minutes for that, then say what to check. After a reboot in the middle, the app runs again on the old database or the restored one, never on half of one.
- A restore that fails says why. Mostly nothing changed, and the app runs again.
- When an app moves to another bucket, its backups start again there. What is in the old bucket stays. So does what is in a bucket you removed from moin.
- Removing an app keeps its backups in your bucket until you delete them.
- To keep a server's backups in one bucket only, use the `backups-to` [rule](/docs/guides/security#rules).
- Not sure which database to pick? [SQLite or Postgres?](/docs/guides/sqlite-or-postgres)
