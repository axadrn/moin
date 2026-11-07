---
title: "How backups work"
description: "Every change, full backups and the daily restore check, and what lives where in your bucket"
group: "Servers and data"
order: 4
---

moin keeps two kinds of backup of every SQLite and Postgres database, both in your own bucket, and checks every day that they restore. Everything runs in the agent on your server: your data goes from your server to your bucket, never through moin.

- **Every change**, so you can go back to any second of the last 7 days: to 14:01, one minute before the wrong `DELETE`.
- **Full backups**, one file each, that you can open anywhere, even without moin.

## The two kinds

| | SQLite | Postgres |
| --- | --- | --- |
| Every change | [Litestream](https://litestream.io), in the bucket within a second | [WAL-G](https://github.com/wal-g/wal-g), within a minute |
| Full backups | the file, gzipped | `pg_dump` in its custom format, also every hour |
| Daily check | restore into a file, `PRAGMA integrity_check` | restore into a throwaway Postgres, check that every table came back |
| moin finds it | a SQLite file on a volume | a service that runs `postgres`, `postgis/postgis`, `pgvector/pgvector` or `timescale/timescaledb` |

Full backups are taken before every deploy, restore and data edit, and the newest 50 stay. A full SQLite backup is a plain gzipped file: `gunzip` it and open it with `sqlite3`. A Postgres one opens with `pg_restore`. The app shows when the daily check last passed.

## SQLite

The agent runs Litestream for every SQLite file on a volume of the live deploy. Litestream copies each change to the bucket as it happens. Only the agent may run Litestream for a file: a second one for the same file breaks the history.

## Postgres

Next to the Postgres container the agent runs WAL-G in a small companion container that shares its network. It streams every change as Postgres writes it and uploads each finished piece of the log. Once a minute, if there were writes, moin closes the current piece, so a quiet database is in the bucket within a minute too. A full base backup runs every day and before every deploy; WAL-G deletes what the 7 days no longer need.

The daily check restores the newest full backup into a throwaway Postgres of the same image, with no network.

A point in time restore takes a full backup first, rebuilds the data in a new volume from the base backup and the log up to your second, and only when that worked stops the app, swaps the data in and starts it again. If anything fails before the swap, nothing changed. The window starts at the oldest base backup and ends at the newest piece of the log in the bucket.

moin sets `max_slot_wal_keep_size` to 4 GB when it is still the default, so a WAL-G that stays down never fills your disk: past that Postgres drops WAL-G's place, moin takes a new base backup at once, and the app's backups say so for a day.

## A restore

A restore never loses the present. moin first takes a full backup of the current database (`before-restore`), then downloads the backup, checks it, stops the app for the swap and starts it again. To undo a restore, restore `before-restore`. After a restore the point in time history starts again from the restored database.

## Why a bucket

Your laptop and your server never talk to each other directly: the server has no open port, your laptop sits behind a router. They hand things over in your bucket, not on our servers.

```
moin deploy   laptop ──▶ your bucket ──▶ your server     (the server deletes it after)
backups       server ──▶ your bucket                     (every change, plus full backups)
GitHub        GitHub ──────────────────▶ your server     (no bucket needed)
```

moin keeps the bucket's secret key encrypted and uses it for one thing: to sign links that work for a few minutes.

## What lives where

Everything of one workspace goes into one folder named after the workspace's ID, so two workspaces can share a bucket, even with apps of the same name. With a **Folder** set, the whole tree moves one level down, into it.

```
<your-bucket>/
└── <folder>/                     ← only when you set one
    └── <workspace>/              ← your workspace's ID, it never changes
        └── <app-name>/
            └── <app-id>/         ← a new app with the same name gets a new one
                ├── uploads/      ← your code on its way to the server
                └── <database>/   ← one per database: data-app.db for /data/app.db, postgres-db for service db
                    ├── litestream/   ← SQLite: every change of the last 7 days
                    ├── walg/         ← Postgres: every change and the base backups
                    └── snapshots/    ← full backups, the newest 50
```

## How long things stay

| What | How long | Who deletes it |
| --- | --- | --- |
| `uploads/` | until the server has it, 24 hours at most | the agent: right after the download, and once an hour anything older than 24 hours |
| `litestream/`, `walg/` | every change of the last 7 days | the agent, with Litestream's and WAL-G's own cleanup |
| `snapshots/` | the newest 50 per database | the agent, after each new full backup |
| an app you removed | until you delete it | you: its `<app-id>/` folder, in your provider's dashboard or with any S3 tool |
| a bucket or folder you changed | until you delete it | you: moin starts in the new place with a full backup and leaves the old one |
| a bucket you removed from moin | until you delete it | you: moin deletes nothing |
| an app you moved to another bucket | until you delete it | you: moin still lists and restores its older backups from there |

A new app with the name of a removed one starts with an empty folder: old backups never mix with new ones. To get the old data back, download a full backup from the old `<app-id>/` folder and open it.

## Several buckets

A workspace can keep several buckets: a client's data in the client's bucket, a bucket near the server, separate costs, production apart from tests. One is the **default**: every app without a bucket of its own, and every new app, uses it. The first bucket you add is the default; a bucket from before moin had more than one is the default, named `default`.

When an app moves to another bucket:

- **New backups** go to the new bucket. It starts with a full backup of every database, then every change, so a point in time restore reaches back to the move and no further.
- **Older backups stay** in the old bucket. moin moves and deletes nothing, lists them with their bucket, and restores them from there.
- **Uploads** of `moin deploy` go to the new bucket at once.

Making another bucket the default moves every app that follows the default the same way.
