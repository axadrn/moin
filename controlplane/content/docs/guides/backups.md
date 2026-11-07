---
title: "Backups"
description: "Every change of every SQLite and Postgres database in your bucket, checked every day"
group: "Servers and data"
order: 3
---

moin backs up every SQLite and Postgres database of your apps to your own bucket, with nothing to set up: every change, so you can go back to any second of the last 7 days, and a full backup before every deploy, restore and edit. Every day it restores a backup and checks it. [How backups work](/docs/concepts/backups) says how.

```surfaces
Web: the app, **Backups**
CLI: moin backups
TUI: on the app, `b`
API: GET /api/v1/apps/{app}/backups
MCP: get_backups
```

It lists the full backups, how far back a point in time restore reaches, and the last restore check. Backups in a bucket the app left show with that bucket's name. Backups start once the workspace has a [bucket](/docs/guides/bucket#add-it).

## Back up now

Takes a full backup right now, before you try something risky.

```surfaces
Web: the app, **Backups**, **Back up now**
CLI: moin backup
TUI: on the app, `N`
API: POST /api/v1/apps/{app}/backups
MCP: back_up_app
```

## Restore a snapshot

Puts the database back to one full backup. Its ID is the time it was taken, then why, like `20261031T143638Z-manual`: copy it from the list, moin refuses anything else.

```surfaces
Web: the app, **Backups**, **Restore** on a backup
CLI: moin restore <snapshot-id>
TUI: on the app, `b`, pick the backup, `enter`, then type the app name
API: POST /api/v1/apps/{app}/restore
MCP: restore_app
```

A backup in an older bucket, from before the app moved, restores from that bucket: the web and the TUI know it, the CLI takes `--bucket <bucket>`, the API and MCP `bucket`.

## Restore to a point in time

Puts the database back to any second of the last 7 days. Deleted the wrong rows at 14:02? Restore to 14:01.

```surfaces
Web: the app, **Backups**, **Restore to a point in time**: pick the day and the time, then **Restore to this time**
CLI: moin restore --at "2026-10-31 14:01:00"
TUI: on the app, `b`, then `t`: pick the moment, `enter`, then type the app name
API: POST /api/v1/apps/{app}/restore
MCP: restore_app
```

Times are in UTC. A time without a day, like `14:01`, means today. The API and MCP take it as `at`. After a move to another bucket it reaches back to the move.

## A snapshot before every change

On by default: a full backup before moin changes anything, so a bad migration or a wrong `UPDATE` is one click from undone.

```surfaces
Web: the app, **Backups**, **Snapshot before every change**
CLI: moin backups auto off
CLI: moin backups auto on
TUI: on the app, `b`, then `a`
API: PUT /api/v1/apps/{app}/backups/settings
MCP: set_backup_settings
```

It runs before a [deploy](/docs/guides/deploy) (`before-v4`), before a cell edit, a deleted row or SQL with **Allow changes** (`before-sql`), and before a restore (`before-restore`). More in the same second get `before-sql-2`, `before-sql-3`.

## Postgres

Any service that runs Postgres is backed up: no password, no change to your `compose.yml`. The data must be on a volume:

```yaml
services:
  web:
    build: .
    environment:
      DATABASE_URL: postgres://app:${POSTGRES_PASSWORD}@db:5432/shop
  db:
    image: postgres:17
    environment:
      POSTGRES_USER: app
      POSTGRES_DB: shop
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - pgdata:/var/lib/postgresql/data
volumes:
  pgdata:
```

In moin this database is `postgres-db` (`postgres-<service>`): in the bucket, in the app's backups and in `--db postgres-db`.

## Requirements

- **SQLite**: the file is on a volume and uses WAL mode (`PRAGMA journal_mode=WAL`), as most apps do. Do not run a second Litestream for the same file.
- **Postgres**: a service that runs `postgres`, `postgis/postgis`, `pgvector/pgvector` or `timescale/timescaledb`, with its data on a volume and a Debian-based image, like `postgres:17`. Alpine images such as `postgres:17-alpine` get full backups and the daily check but not every change; the app's backups say so.
- No database shows up? See [Data](/docs/guides/data#requirements).

## Good to know

- A restore replaces the live data, so moin asks you to type the app's name first, on the web, in the CLI and in the TUI. `--yes` skips it in scripts; an agent passes the name as `confirm`.
- A restore first takes a full backup of the present (`before-restore`): restore that to undo it.
- An app with more than one database: the CLI restores the one you name with `--db /data/app.db` or `--db postgres-db`.
- Full backups: the newest 50 per database stay. Every change: 7 days. Postgres also gets a full backup every hour.
- A full SQLite backup is a gzipped file: `gunzip` it and open it with `sqlite3`, no moin needed. A Postgres one opens with `pg_restore`.
- Removing the bucket from moin stops its backups; what is in it stays. See [Remove a bucket](/docs/guides/bucket#remove-a-bucket).
- To keep a server's backups in one bucket only, use the `backups-to` [rule](/docs/guides/security#rules).
