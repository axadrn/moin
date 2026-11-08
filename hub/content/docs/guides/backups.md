---
title: "Backups"
description: "Copies on your schedule, go back to one, undo it, and download a copy"
group: "Servers and data"
order: 3
---

moin copies your database into your bucket, on a schedule you pick. It keeps the copies as long as you say. Every day it checks that a copy restores.

With SQLite it can also write down every change, so you can go back to any minute.

```
your server ──copies──▶ your bucket
     ▲                      │
     └──── a restore ◀──────┘

hub: how often, how long,
the list. Never your data.
```

## What moin backs up

moin backs up databases, not files.

It backs up every SQLite and Postgres database of your apps, once the workspace has a [bucket](/docs/guides/bucket#add-it).

Every hour moin looks for new databases. So a SQLite file your app makes long after the deploy gets backed up too.

What looks like a database but cannot be backed up shows in red: MySQL, MongoDB, or Postgres in an image moin does not know.

## See your backups

One line says how each database stands: "Last copy 12m ago · checked 2h ago". Or, in red, why it does not work. Then the moments you can go back to.

```surfaces
Web: the app, **Data**, a database
CLI: moin backups my-app
TUI: on the app, `b`
API: GET /api/v1/apps/{app}/backups
MCP: get_backups
```

## How often and how long

Three settings per app, for all of its databases:

- **How often**: every 15 minutes, hourly or daily. Hourly unless you change it.
- **Keep**: 1, 3, 7 or 30 days. 7 unless you change it. The newest copy always stays.
- **Every change** (SQLite only): on unless you turn it off. It writes down every change between the copies. Turning it off removes what it wrote down. The copies stay.

```surfaces
Web: the app, **Data**, a database: **How often**, **Keep**, **Every change**
CLI: moin backups my-app --every 15m --keep 30d --every-change on
TUI: on the app, `b`; the settings open on the web
API: PUT /api/v1/apps/{app}/backups
MCP: set_backups
```

The CLI takes `15m`, `1h` or `1d`; `1d`, `3d`, `7d` or `30d`; `on` or `off`. The API and MCP take `every_minutes` (15, 60 or 1440), `keep_days` (1, 3, 7 or 30) and `every_change`.

The schedule can also live in your repo:

```yaml
x-moin:
  backups:
    every: 15m
    keep: 30d
    every_change: true
```

Then the file decides. moin shows the schedule locked, marked **from compose.yml**. Changing it elsewhere answers `schedule_in_file`. [Who decides](/docs/guides/config#who-decides).

## Extra copies

moin also takes one copy right before each of these:

- every deploy
- every [catalog](/docs/guides/catalog) update
- every [data](/docs/guides/data) edit you make in moin

A database over 1 GB gets no extra copy before a data edit. Deploys still take one.

The moments you can go back to have names:

- **At 14:00**: a copy from your schedule. From another day: **Nov 6 at 14:00**
- **Before v12**: right before a deploy started v12
- **Before the update to 3.4.1**: right before a catalog update
- **Before your SQL change**, **Before your edit in users**, **Before the row added to users**: right before a data edit
- **1 minute ago**, **15 minutes ago**, **1 hour ago**, **Yesterday this time**: SQLite with Every change
- **Before the restore at 14:05**: going back to it undoes the restore

Times are in UTC. [How a copy is made](/docs/concepts/backups#how-a-copy-is-made).

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

`--to` takes a moment from `moin backups`, by its ID or label: `1h`, `before-v12`, `20261107T140000Z`. Or a time in UTC: `14:01` for today, or `"2026-11-07 14:01"`. SQLite with Every change goes to that minute. Everything else takes the newest copy at or before it.

The API and MCP take `moment` or `at`. Two moments with the same label need the moment's ID. With more than one database, name one: `--db /data/app.db` or `--db postgres-db`.

## Undo a restore

Every restore keeps the database as it was right before, as long as you keep copies. Go back to **Before the restore at 14:05** to undo it. An undo is a restore too, so you can undo the undo.

```surfaces
Web: the app, **Data**, a database: **Before the restore at 14:05**, **Restore**
CLI: moin restore my-app --to undo-20261107T140500Z
TUI: on the app, `b`, pick **Before the restore at …**, `enter`
API: POST /api/v1/apps/{app}/restores
MCP: restore_app
```

## Download a copy

A link to a copy in your bucket: the newest one, or the one you name. It works for 15 minutes. The file comes straight from your bucket, not through moin.

```surfaces
Web: the app, **Data**, a database, **Download a copy**
CLI: moin backup download my-app
TUI: on the app, `b`, then `c`
API: POST /api/v1/apps/{app}/backups/download
MCP: download_backup
```

The CLI saves it in the current folder. `--link` prints the link instead. `--to before-v12` takes another copy.

A SQLite copy is gzipped: `gunzip` it and open it with `sqlite3`. A Postgres copy is a `pg_dump`: `pg_restore` puts it into any Postgres. You need no moin for either.

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

- **SQLite**: the file is in a mounted folder or volume. Every change needs WAL mode (`PRAGMA journal_mode=WAL`). Do not run a second Litestream for the same file.
- **Postgres**: a service that runs one of the [known images](/docs/guides/data#requirements). Keep its data on a volume.

## Good to know

- A restore replaces the live data, so you type the app's name first. The hub checks it. Scripts pass it with `--confirm <app>`; `--yes` is not enough.
- A restore checks first that the disk has room. It reads the size noted on the copy.
- A restore goes on when the server goes offline or the hub restarts. The CLI and the TUI wait 10 minutes for that, then say what to check. After a reboot in the middle, the app runs again on the old database or the restored one, never on half of one.
- A restore that fails says why. Mostly nothing changed, and the app runs again.
- When an app moves to another bucket, its copies start again there. What is in the old bucket stays. So does what is in a bucket you removed from moin.
- Removing an app keeps its copies in your bucket until you delete them.
- To keep a server's backups in one bucket only, use the `backups-to` [rule](/docs/guides/security#rules).
- Not sure which database to pick? [SQLite or Postgres?](/docs/guides/sqlite-or-postgres)
