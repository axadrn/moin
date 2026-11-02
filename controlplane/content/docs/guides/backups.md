---
title: "Backups"
description: "Every change of every SQLite database in your own bucket, restored to any second"
order: 7
---

ops backs up every SQLite database of your apps into your own bucket, two ways at once, and checks every day that the backups restore. A bucket is a folder in an online storage service such as Cloudflare R2 or AWS S3; [Your bucket](/docs/guides/bucket) explains why ops uses one.

- **Continuous.** The agent copies every change to your bucket about once a second, with [Litestream](https://litestream.io). You can restore to any point in time of the last 7 days: any second, such as 14:01:00 yesterday.
- **Snapshots.** A snapshot is a full copy of the database at one moment, gzipped. ops takes one before every deploy, before every restore, before every change you make through ops, and whenever you ask. It keeps the newest 50 per database.
- **Restore checks.** Once a day the agent restores the newest state into a temporary file and runs SQLite's integrity check on it. The app shows when it last passed.

Everything runs in the agent on your server. Your data goes from your server to your bucket and never through ops.

## Set up the bucket

Tells ops which bucket to back up into. One bucket serves all your apps, and it is the same bucket `ops deploy` uses.

```surfaces
Web: **Bucket** in the sidebar, then **Save bucket**
CLI: ops backups bucket set \
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --region auto \
  --bucket <bucket-name> \
  --access-key <access-key>
TUI: settings tab, **Bucket**, then `enter`
API: PUT /api/v1/backup-storage
MCP: set_backup_storage
```

Any S3-compatible storage works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner Object Storage, Exoscale, MinIO. Create a bucket and a key that may read, write and delete in it first. The endpoint must start with `https://`. For AWS S3, leave `--endpoint` out and set `--region` to the bucket's region. The CLI asks for the secret key on stdin. [Add it](/docs/guides/bucket#add-it) explains every field.

ops writes and deletes a test file first, so a wrong key fails right away. The secret key is stored encrypted. Every online server starts backing up at once, the others when they connect.

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

- **litestream:** every change as it happens, so you can go back to any second of the last 7 days. The ops agent on your server cleans up changes older than that.
- **snapshots:** full copies before every deploy and change, the last 50. After each new one the agent deletes the oldest.

[What goes in the bucket](/docs/guides/bucket#what-goes-in-the-bucket) shows the whole tree, with a folder and with uploads, and what happens to backups when you remove an app or change the bucket.

A snapshot is a plain gzipped SQLite file: `gunzip` it and open it with `sqlite3`, no ops needed.

## See the bucket

Shows which bucket ops uses, without its secret key.

```surfaces
Web: **Bucket** in the sidebar
CLI: ops backups bucket
TUI: settings tab
API: GET /api/v1/backup-storage
MCP: get_backup_storage
```

## See the backups of an app

Lists the snapshots, how far back a point in time restore reaches, and the last restore check.

```surfaces
Web: the app page, **Backups** tab
CLI: ops backups
TUI: on the app, `b`
API: GET /api/v1/apps/{app}/backups
MCP: get_backups
```

## Back up now

Takes a snapshot right now, for example before you try something risky.

```surfaces
Web: the app page, **Backups** tab, **Back up now**
CLI: ops backup
TUI: on the app, `b`, then `enter`
API: POST /api/v1/apps/{app}/backups
MCP: back_up_app
```

## Restore a snapshot

Puts the database back to the state of one snapshot.

```surfaces
Web: the app page, **Backups** tab, **Restore** on a snapshot
CLI: ops restore 20261031T143638Z-manual
TUI: on the app, `b`, type the snapshot ID, `enter`, then the app name
API: POST /api/v1/apps/{app}/restore
MCP: restore_app
```

## Restore to a point in time

Puts the database back to any second of the last 7 days. Deleted the wrong rows at 14:02? Restore to 14:01.

```surfaces
Web: the app page, **Backups** tab, **Restore to a point in time**: pick the day and the time, then **Restore to this time**
CLI: ops restore --at "2026-10-31 14:01:00"
TUI: on the app, `b`, type `14:01`, `enter`, then the app name
API: POST /api/v1/apps/{app}/restore
MCP: restore_app
```

Times are in UTC, not your local time. A time without a day, like `14:01`, means today. In the API and MCP, send the time as `at`.

If the app has more than one database, the CLI restores the one you name with `--db /data/app.db`.

## What a restore does

A restore never loses the present. ops first takes a snapshot of the current database (`before-restore`), then downloads the backup, checks it, stops the app for the swap and starts it again. To undo a restore, restore that snapshot.

After a restore, the point in time history starts again from the restored database.

A restore replaces the live data, so ops asks you to type the app's name first, on the web, in the CLI and in the TUI. `--yes` skips it in scripts. An agent passes the name as `confirm`.

## A snapshot before every change

Takes a snapshot before ops changes anything, so a bad migration or a wrong `UPDATE` is one click from undone. It is on by default.

```surfaces
Web: the app page, **Backups** tab, **Snapshot before every change**
CLI: ops backups auto off
CLI: ops backups auto on
TUI: on the app, `b`, then `on` or `off`
API: PUT /api/v1/apps/{app}/backups/settings
MCP: set_backup_settings
```

ops then takes a snapshot before a deploy (`before-v4`), before a cell edit, a deleted row or SQL with **Allow changes** (`before-sql`), and before a restore. More changes in the same second each get their own snapshot: `before-sql-2`, `before-sql-3`.

## Stop backups

Removes the bucket from ops, so no server backs up any more. What is in the bucket stays there.

```surfaces
Web: **Bucket** in the sidebar, **Remove bucket**
CLI: ops backups bucket rm
TUI: settings tab, **Bucket**, `x`, then type the bucket name
API: DELETE /api/v1/backup-storage
MCP: remove_backup_storage
```

ops asks you to type the bucket's name first; `--yes` skips it in scripts.

## Requirements

The app keeps SQLite on a volume and uses WAL mode, as most apps do. WAL mode is a SQLite setting that lets two programs read and write at the same time; your app turns it on with `PRAGMA journal_mode=WAL`. Only the agent runs Litestream for a database: do not run a second Litestream for the same file.

No database shows up? See [Data](/docs/guides/data#requirements): ops only finds SQLite files on a volume of the live release.
