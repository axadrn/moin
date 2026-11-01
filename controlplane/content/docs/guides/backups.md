---
title: "Backups"
description: "Every change of every SQLite database in your own bucket, restored to any second"
order: 7
---

ops backs up every SQLite database of your apps into your own S3-compatible bucket, two ways at once, and proves every day that the backups restore.

- **Continuous.** The agent replicates every change with [Litestream](https://litestream.io), about once a second. You restore to any second of the last 7 days.
- **Snapshots.** A full copy of the database, made with `VACUUM INTO` and gzipped: before every deploy, before every restore, before every change you make through ops, and whenever you ask. ops keeps the newest 50 per database.
- **Restore drills.** Once a day the agent restores the newest state into a temp file and runs SQLite's integrity check on it. The app shows **Verified by restore** with the time it last passed.

Everything runs inside the agent on your server. Your data goes from your server to your bucket and never through ops.

| Action | Web | CLI | TUI | MCP |
| --- | --- | --- | --- | --- |
| Set the bucket | app page, **Backups** | `ops backups bucket set …` | – | `set_backup_storage` |
| List backups | **Backups** | `ops backups` | `b` | `get_backups` |
| Back up now | **Back up now** | `ops backup` | `b`, then `enter` | `back_up_app` |
| Restore a snapshot | **Restore** on a snapshot | `ops restore <snapshot>` | `b`, type the ID, `enter`, `y` | `restore_app` |
| Restore a point in time | **Back to a point in time** | `ops restore --at "2026-10-31 14:01"` | `b`, type `14:01`, `enter`, `y` | `restore_app` with `at` |
| Snapshot before changes | **Before every change** | `ops backups auto on\|off` | – | `set_backup_settings` |
| Stop backups | **Stop backups** | `ops backups bucket rm` | – | `remove_backup_storage` |

## Set up the bucket

Backups go to [your bucket](/docs/guides/bucket), the same one `ops deploy` uses. One bucket serves all your apps. Any S3-compatible store works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner Object Storage, MinIO. Create a bucket and a key that may read and write it, then:

```bash
ops backups bucket set \
  --endpoint https://<account>.r2.cloudflarestorage.com \
  --region auto \
  --bucket ops-backups \
  --access-key <key id>
# the secret key comes on stdin
```

Leave `--endpoint` out for AWS S3. ops writes and deletes a test file first, so a wrong key fails right away. The secret key is stored encrypted with AES-256-GCM; every online server starts backing up at once, the others when they connect.

In the bucket each database gets a folder:

```
<folder>/<app>/data-app.db/litestream/   every change
<folder>/<app>/data-app.db/snapshots/    20261031T143638Z-manual.db.gz
```

A snapshot is a plain gzipped SQLite file: `gunzip` it and open it with `sqlite3`, no ops needed.

## Restore

A restore never loses the present: ops first takes a snapshot of the current database (`before-restore`), then downloads the backup, runs the integrity check on it, stops the app for the swap and starts it again. Undoing a restore is restoring that snapshot.

```bash
ops backups                                  # snapshots and the point in time window
ops restore 20261031T143638Z-manual          # from a snapshot
ops restore --at "2026-10-31 14:01:00"       # to a second, in UTC
```

After a restore, the point in time history starts again from the restored database.

## A snapshot before every change

On by default. Before ops changes anything, it takes a snapshot: before a deploy (`before-v4`), before a cell edit, a deleted row or SQL with **Allow changes** (`before-sql`), and before a restore. A bad migration or a wrong `UPDATE` is one click away from undone. Turn it off per app with `ops backups auto off`.

## Requirements

The app keeps SQLite on a volume and uses WAL mode, as most apps do. Only the agent runs Litestream for a database; do not run a second Litestream for the same file.
