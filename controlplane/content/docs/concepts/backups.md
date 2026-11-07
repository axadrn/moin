---
title: "How backups work"
description: "Copies on your schedule in your own bucket, a daily check, and what lives where"
group: "Servers and data"
order: 4
---

moin copies your database into your bucket on a schedule you pick, and keeps the copies as long as you say. With SQLite it can also write down every change, so you go back to any minute. Every day moin checks that a copy restores.

That is the whole idea. Two settings per app: **how often** (every 15 minutes, hourly or daily; hourly unless you change it) and **how long to keep** (1, 3, 7 or 30 days; 7 unless you change it). [Backups](/docs/guides/backups) says how to set them.

## Where the work happens

On your server. Your data goes from your server to your bucket, never through moin. The agent on your server makes each copy and puts it in your bucket. The control plane only keeps your two settings and shows you the list.

```
control plane
  keeps: how often, how long
  shows: the list
  holds: no data

your server ──copies──▶ your bucket
```

## How a copy is made

**SQLite.** moin uses `VACUUM INTO`: SQLite's own way to make a clean copy while your app keeps running. `VACUUM INTO` can only write into a file, like a photocopier that needs paper. So the copy is a file on your server for a moment; moin uploads it to your bucket and throws it away.

```
SQLite

your server       your bucket
┌────────────┐    ┌─────────┐
│ app.db     │    │ copies/ │
│  │         │    │         │
│  ▼         │    │         │
│ temp copy ─┼───▶│ 14:00   │
│ (deleted   │gzip│         │
│  at once)  │    │         │
└────────────┘    └─────────┘
```

The temp file sits next to the database and goes right after the upload, also when the upload fails. The copy is gzipped (made smaller) on its way.

**Postgres.** `pg_dump` is the same for Postgres: a clean copy while your app keeps running. `pg_dump` writes a stream, like water from a tap. moin points the tap straight at your bucket, so nothing waits on your server's disk. A big database needs no extra room on the server.

```
Postgres

your server       your bucket
┌────────────┐    ┌─────────┐
│ Postgres   │    │ copies/ │
│  │         │    │         │
│  ▼         │    │         │
│ pg_dump ───┼───▶│ 14:00   │
│            │flow│         │
└────────────┘    └─────────┘
nothing waits on the disk
```

`pg_dump` comes with every Postgres image, Alpine too, so every Postgres is backed up. moin runs it inside your Postgres container: no password leaves it. It copies every database in it, each into a file of its own, so each one goes back on its own.

## Every change (SQLite)

Copies are a few minutes or hours apart. For SQLite, one switch also writes down every change between them: **Every change**, on unless you turn it off. [Litestream](https://litestream.io), a small tool inside the agent, sends each change to your bucket within a second. Then you can go back to any minute, not only to a copy.

It keeps the changes as long as you keep the copies. Turning it off removes what it wrote down; the copies stay. Postgres goes back to its copies only.

## Going back

The list **Go back to** offers, newest first:

- **1 minute ago**, **15 minutes ago**, **1 hour ago**, **Yesterday this time**: SQLite with Every change only.
- **At 14:00**: a copy from your schedule.
- **Before v12**, **Before the update to 3.4.1**, **Before your SQL change**: moin takes one extra copy right before every deploy, every catalog update and every data edit you make in moin (SQL, a cell, a row). So "Before v12" is exactly the data v12 found.
- **Before the restore at 14:05**: the database as it was before a restore. Going back to it undoes that restore.

A time works too, like 14:01: SQLite with Every change goes to that minute, everything else to the newest copy at or before it.

## A restore

A restore never changes your data in place. moin builds the database beside the live one, switches in one step, and keeps the old one as the undo.

```
1 build beside, the app runs
  app.db           live
  app.db.restore   built

2 stop the app, switch
  app.db  ─▶ app.db.before-…
  app.db.restore ─▶ app.db

3 start the app
  app.db           restored
  app.db.before-…  the undo
```

The same for Postgres, with databases instead of files:

```
1 build beside, the app runs
  shop             live
  shop_restore     built

2 stop the app, switch
  shop  ─▶ shop_before_…
  shop_restore ─▶ shop

3 start the app
  shop             restored
  shop_before_…    the undo
```

- **SQLite.** moin unpacks the copy into `app.db.restore` next to the live file (or, for a minute, has Litestream rebuild it there) and checks it. Then it stops the app, folds the live file's log into it, renames it to `app.db.before-<time>` and the restored file to `app.db`, and starts the app. The stop lasts a moment.
- **Postgres.** The copy flows from your bucket straight into `pg_restore`, into a new database `shop_restore` in the same Postgres, while your app keeps running. Then moin stops the app's other services (the Postgres keeps running), renames `shop` to `shop_before_<time>` and `shop_restore` to `shop`, and starts them again.

The undo stays as long as you keep copies, then moin removes it. Undo = the same switch the other way.

**If the server dies in the middle.** Before it stops the app, moin writes a small note: "restoring". When the agent starts again it reads the note and follows one rule: the live database is missing and the old one is there, so the old one goes back. Then it starts the app and tells you how the restore ended. Your app always runs on a whole database, the one before or the restored one, never half of one.

## The daily check

Every day moin takes the newest copy from your bucket and restores it into a throwaway database on your server. SQLite: a temp file and SQLite's own `PRAGMA integrity_check`, which reads every page. Postgres: a throwaway database `shop_check` where moin reads every row of every table, then drops it. The app shows when the check last ran, and an [alert](/docs/guides/alerts) goes out when it fails. "Backed up" means "copied, then restored and read".

## A copy for yourself

**Download a copy** gives you a link to a copy in your bucket, the newest or one you pick. The link works for 15 minutes; the file comes straight from your bucket, not through moin. SQLite comes gzipped (`gunzip`, then `sqlite3`), Postgres as a `pg_dump` (`pg_restore`). You need no moin to open it.

## What lives where

Everything of one workspace goes into one folder named after the workspace's ID, so two workspaces can share a bucket, even with apps of the same name.

```
<your bucket>/
└─ <folder>/
   └─ <workspace>/
      └─ <app>/
         └─ <app-id>/
            ├─ uploads/
            └─ <database>/
               ├─ copies/
               └─ litestream/
```

| Folder | What is in it |
| --- | --- |
| `<folder>/` | only when you set a **Folder** on the bucket |
| `<workspace>/` | your workspace's ID; it never changes |
| `<app-id>/` | a new app with the same name gets a new one, so old copies never mix with new |
| `uploads/` | your code on its way to the server (`moin deploy`) |
| `<database>/` | one per database: `data-app.db` for `/data/app.db`, `postgres-db` for the service `db` |
| `copies/` | the copies, named by their time in UTC: `20261107T140000Z.db.gz` for SQLite, `20261107T140000Z.dump` for Postgres |
| `litestream/` | SQLite with Every change: every change |

## How long things stay

The agent deletes what is too old, once a minute after a copy and once an hour for uploads:

- **Copies** and **Every change**: as long as the app keeps copies (**Keep**). The newest copy always stays, so a database that stopped changing is never left without one.
- **The undo of a restore** (`app.db.before-…` next to the file, `shop_before_…` in the same Postgres): as long as the app keeps copies.
- **Uploads**: until the server has them, a day at most.

moin never deletes the rest:

- **An app you removed** keeps its folder and its copies until you delete them, in your provider's dashboard or with any S3 tool.
- **Folders from an older moin** (`snapshots/`, `walg/`, `before-restore/`, `downloads/`) stay as they are. moin no longer reads them; delete them when you like.
- **A bucket you left** keeps what is in it.

## Several buckets

A workspace can keep several buckets: a client's data in the client's bucket, a bucket near the server, separate costs, production apart from tests. One is the **default**: every app without a bucket of its own, and every new app, uses it.

When an app moves to another bucket, its copies start again there. What is in the old bucket stays; moin deletes nothing.

## Under the hood

The agent does it all, with one small set of verbs and one file per kind of database (`backup_sqlite.go`, `backup_postgres.go`); `backup.go` holds the schedule, the cleanup and the restore note:

| Verb | SQLite | Postgres |
| --- | --- | --- |
| **Copy** | `VACUUM INTO` a temp file, gzip, upload | `pg_dump -Fc` straight into the upload |
| **Restore** | build `app.db.restore`, rename twice | `pg_restore` into `shop_restore`, rename twice |
| **Check** | `PRAGMA integrity_check` | read every table of `shop_check` |
| **Record** | Litestream, with Every change | |

Once a minute the agent looks at each database: when the newest copy is older than **how often**, it takes one and deletes copies and undos older than **how long to keep**.
