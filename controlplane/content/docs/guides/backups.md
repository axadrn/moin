---
title: "Backups"
description: "Two backups of every SQLite and Postgres database in your own bucket, checked every day"
order: 7
---

Two backups. Always.

- **Every change.** In your bucket within a second for SQLite, within a minute for Postgres. Go back to any second of the last 7 days.
- **Full copies.** Before every deploy, restore and edit, and every hour for Postgres. The newest 50 stay.
- **Checked daily.** Every day moin restores your backup and checks it. The app shows when it last passed.

Why both? Every change lets you go back to 14:01, one minute before the wrong `DELETE`. A full copy is one file you can open anywhere, even without moin.

Everything runs in the agent on your server. Your data goes from your server to your bucket, never through moin. A bucket is a folder in an online storage service such as Cloudflare R2 or AWS S3; [Your bucket](/docs/guides/bucket) explains why moin uses one.

| | SQLite | Postgres |
| --- | --- | --- |
| Every change | [Litestream](https://litestream.io), within a second | [WAL-G](https://github.com/wal-g/wal-g), within a minute |
| Full copies | the file, gzipped | `pg_dump`, also every hour |
| Daily check | restore into a file, `PRAGMA integrity_check` | restore into a throwaway Postgres, count the tables |
| moin finds it | a SQLite file on a volume | a service that runs `postgres`, `postgis/postgis`, `pgvector/pgvector` or `timescale/timescaledb` |

## Set up the bucket

Tells moin which bucket to back up into. One bucket serves all your apps, and it is the same bucket `moin deploy` uses. An app can also [have its own bucket](/docs/guides/bucket#more-than-one-bucket).

```surfaces
Web: **Buckets** in the sidebar, then **Add bucket**
CLI: moin buckets add default \
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --region auto \
  --bucket <bucket-name> \
  --access-key <access-key>
TUI: settings tab, `b`
API: POST /api/v1/buckets
MCP: add_bucket
```

Any S3-compatible storage works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner Object Storage, Exoscale, MinIO. Create a bucket and a key that may read, write and delete in it first. The endpoint must start with `https://`. For AWS S3, leave `--endpoint` out and set `--region` to the bucket's region. The CLI asks for the secret key on stdin. [Add it](/docs/guides/bucket#add-it) explains every field.

moin writes and deletes a test file first, so a wrong key fails right away. The secret key is stored encrypted. Every online server starts backing up at once, the others when they connect.

In the bucket each database of an app gets a folder with two folders inside:

```
<your-bucket>/
└── <workspace>/                  ← your workspace's ID, it never changes
    └── <app-name>/               ← the name of your app
        └── <app-id>/             ← this app's ID
            └── <database-file>/  ← one per database (e.g. data-app.db for /data/app.db)
                ├── litestream/   ← every change, any second of the last 7 days
                └── snapshots/    ← full copies, the newest 50
```

- **litestream:** every change as it happens, so you can go back to any second of the last 7 days. The moin agent on your server cleans up changes older than that.
- **snapshots:** full copies before every deploy and change, the last 50. After each new one the agent deletes the oldest.

[What goes in the bucket](/docs/guides/bucket#what-goes-in-the-bucket) shows the whole tree, with a folder and with uploads, and what happens to backups when you remove an app or change the bucket.

A snapshot is a plain gzipped SQLite file: `gunzip` it and open it with `sqlite3`, no moin needed.

## See the buckets

Shows which buckets moin uses, without their secret keys, and which apps use each one. The **Backups** tab of an app shows its bucket.

```surfaces
Web: **Buckets** in the sidebar
CLI: moin buckets
TUI: settings tab
API: GET /api/v1/buckets
MCP: list_buckets
```

## Move an app to another bucket

Its uploads and new backups go to the other bucket; its older backups stay where they are, and moin still lists them and restores them from there. [Choose the bucket of an app](/docs/guides/bucket#choose-the-bucket-of-an-app) explains it.

```surfaces
Web: the app page, **Backups** tab, **Use another bucket**
CLI: moin buckets use client-a [app]
TUI: on the app, `b`, then `bucket client-a`
API: PUT /api/v1/apps/{app}/bucket
MCP: set_app_bucket
```

## See the backups of an app

Lists the snapshots, how far back a point in time restore reaches, and the last restore check. Snapshots in a bucket the app left show with that bucket's name.

```surfaces
Web: the app page, **Backups** tab
CLI: moin backups
TUI: on the app, `b`
API: GET /api/v1/apps/{app}/backups
MCP: get_backups
```

## Back up now

Takes a snapshot right now, for example before you try something risky.

```surfaces
Web: the app page, **Backups** tab, **Back up now**
CLI: moin backup
TUI: on the app, `b`, then `enter`
API: POST /api/v1/apps/{app}/backups
MCP: back_up_app
```

## Restore a snapshot

Puts the database back to the state of one snapshot.

A snapshot ID is the time it was taken, then why, like `20261031T143638Z-manual`. Copy it from the snapshot list: moin refuses anything else.

```surfaces
Web: the app page, **Backups** tab, **Restore** on a snapshot
CLI: moin restore <snapshot-id>
TUI: on the app, `b`, type the snapshot ID, `enter`, then the app name
API: POST /api/v1/apps/{app}/restore
MCP: restore_app
```

A snapshot in an older bucket, from before the app moved, restores from that bucket: the web knows it, the CLI takes `--bucket <bucket>`, the TUI `<snapshot-id> <bucket>`, the API and MCP `bucket`.

## Restore to a point in time

Puts the database back to any second of the last 7 days. Deleted the wrong rows at 14:02? Restore to 14:01.

```surfaces
Web: the app page, **Backups** tab, **Restore to a point in time**: pick the day and the time, then **Restore to this time**
CLI: moin restore --at "2026-10-31 14:01:00"
TUI: on the app, `b`, type `14:01`, `enter`, then the app name
API: POST /api/v1/apps/{app}/restore
MCP: restore_app
```

A point in time comes from the app's bucket: after a move it reaches back to the move. Times are in UTC, not your local time. A time without a day, like `14:01`, means today. In the API and MCP, send the time as `at`. For Postgres the window starts at the oldest base backup and ends at the newest piece of the log in the bucket.

If the app has more than one database, the CLI restores the one you name with `--db /data/app.db` or `--db postgres-db`.

## What a restore does

A restore never loses the present. moin first takes a snapshot of the current database (`before-restore`), then downloads the backup, checks it, stops the app for the swap and starts it again. To undo a restore, restore that snapshot.

After a restore, the point in time history starts again from the restored database.

A restore replaces the live data, so moin asks you to type the app's name first, on the web, in the CLI and in the TUI. `--yes` skips it in scripts. An agent passes the name as `confirm`.

## A snapshot before every change

Takes a snapshot before moin changes anything, so a bad migration or a wrong `UPDATE` is one click from undone. It is on by default.

```surfaces
Web: the app page, **Backups** tab, **Snapshot before every change**
CLI: moin backups auto off
CLI: moin backups auto on
TUI: on the app, `b`, then `on` or `off`
API: PUT /api/v1/apps/{app}/backups/settings
MCP: set_backup_settings
```

moin then takes a snapshot before a [deploy](/docs/guides/deploy) (`before-v4`), before a cell edit, a deleted row or SQL with **Allow changes** (`before-sql`), and before a restore. More changes in the same second each get their own snapshot: `before-sql-2`, `before-sql-3`.

## Stop backups

Removing a bucket from moin stops its backups. What is in the bucket stays there.

```surfaces
Web: **Buckets** in the sidebar, **Remove** on the bucket
CLI: moin buckets rm <name>
TUI: settings tab, `x` on the bucket, then type its name
API: DELETE /api/v1/buckets/{name}
MCP: remove_bucket
```

moin refuses a bucket that apps still use or that keeps older backups of an app, and names the apps: move them to another bucket first. [Remove a bucket](/docs/guides/bucket#remove-a-bucket) has the rules. moin asks you to type the bucket's name first; `--yes` skips it in scripts.

## Postgres

moin backs up every service of an app that runs Postgres, with nothing to set up: no password, no change to your `compose.yml`.

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

In moin the database is `postgres-<service>`, here `postgres-db`: in the bucket, on the Backups tab, and in `--db postgres-db` on the CLI.

- **Every change.** Next to the Postgres container the agent runs WAL-G in a small companion container that shares its network. It streams every change as Postgres writes it and uploads each finished piece of the log. Once a minute, if there were writes, moin closes the current piece, so a quiet database is in the bucket within a minute too. A full base backup runs every day and before every deploy; WAL-G deletes what the 7 days no longer need.
- **Full copies.** `pg_dump` in its custom format, every hour and before every deploy, restore and edit. Open one anywhere with `pg_restore`.
- **Checked daily.** moin restores the newest copy into a throwaway Postgres of the same image, with no network, and checks that every table came back.
- **Restore to a point in time.** moin takes a full copy first, rebuilds the data in a new volume from the base backup and the log up to your second, and only when that worked stops the app, swaps the data in and starts the app again. If anything fails before the swap, nothing changed.

moin sets `max_slot_wal_keep_size` to 4 GB when it is still the default, so a WAL-G that stays down never fills your disk: past that Postgres drops WAL-G's place, moin takes a new base backup at once, and the Backups tab says so for a day.

Requirements: the data is on a volume, as above, and the image is Debian-based, like `postgres:17`. WAL-G does not run on Alpine images such as `postgres:17-alpine`: they get full copies and the daily check, and the Backups tab says so.

In the bucket the database's folder holds `walg/` (every change and the base backups) next to `snapshots/`.

## Requirements

The app keeps SQLite on a volume and uses WAL mode, as most apps do. WAL mode is a SQLite setting that lets two programs read and write at the same time; your app turns it on with `PRAGMA journal_mode=WAL`. Only the agent runs Litestream for a database: do not run a second Litestream for the same file.

No database shows up? See [Data](/docs/guides/data#requirements): moin finds SQLite files on a volume of the live release and services that run Postgres. For Postgres, see [Postgres](#postgres).
