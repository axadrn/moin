---
title: "How backups work"
description: "Copies on your schedule in your own bucket, a daily check, and what lives where"
group: "Servers and data"
order: 4
---

moin copies your database into your bucket on a schedule you pick, and keeps the copies as long as you say. With SQLite it can also write down every change, so you go back to any minute. Every day moin checks that a copy restores.

That is the whole idea. Two settings per app: **how often** (every 15 minutes, hourly or daily; hourly unless you change it) and **how long to keep** (1, 3, 7 or 30 days; 7 unless you change it). [Backups](/docs/guides/backups) says how to set them.

## Where the work happens

On your server. Your data goes from your server to your bucket, never through moin. The daemon on your server makes each copy and puts it in your bucket. The hub only keeps your two settings and shows you the list.

```
hub
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

The temp file sits next to the database and goes right after the upload, also when the upload fails; if the daemon stops in the middle, it goes at the next start. So the disk next to the database needs room for one more copy of it for a moment: moin looks first and says so when there is not enough. The copy is gzipped (made smaller) on its way. A copy gets an hour; one that takes longer stops, so it never holds up the next.

**Postgres.** `pg_dump` is the same for Postgres: a clean copy while your app keeps running. `pg_dump` writes a stream, like water from a tap. moin points the tap straight at your bucket, so nothing waits on your server's disk. A copy needs no extra room on the server.

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

Copies are a few minutes or hours apart. For SQLite, one switch also writes down every change between them: **Every change**, on unless you turn it off. [Litestream](https://litestream.io), a small tool inside the daemon, sends each change to your bucket within a second. Then you can go back to any minute, not only to a copy.

It keeps the changes as long as you keep the copies. Turning it off removes what it wrote down; the copies stay. Postgres goes back to its copies only.

## Going back

The list **Go back to** offers, newest first:

- **1 minute ago**, **15 minutes ago**, **1 hour ago**, **Yesterday this time**: SQLite with Every change only.
- **At 14:00**: a copy from your schedule.
- **Before v12**, **Before the update to 3.4.1**, **Before your SQL change**: moin takes one extra copy right before every deploy, every catalog update and every data edit you make in moin (SQL, a cell, a row). So "Before v12" is the data just before v12 started. If that one copy fails, the deploy log says so and the deploy goes on; the scheduled copies are still there.
- **Before the restore at 14:05**: the database as it was before a restore. Going back to it undoes that restore.

A time works too, like 14:01: SQLite with Every change goes to that minute, everything else to the newest copy at or before it.

## A restore

A restore never changes your data in place. moin builds the database beside the live one, switches in one atomic step, and keeps the old one as the undo. There is never a moment without a live database: before the switch it is the old one, after it the new one.

```
1 build beside, the app runs
  app.db           live
  app.db.moin-restore  built

2 stop the app, switch
  app.db ═ also named
    app.db.moin-before-…
  app.db.moin-restore ─▶
    app.db (one step)

3 start the app
  app.db  restored
  app.db.moin-before-…
          the undo
```

The same for Postgres, with databases instead of files:

```
1 build beside, the app runs
  my-app           live
  moin_restore_…   built

2 stop the app, switch
  in one transaction:
  my-app ─▶ moin_before_…
  moin_restore_… ─▶ my-app

3 start the app
  my-app           restored
  moin_before_…    the undo
```

- **SQLite.** moin unpacks the copy into `app.db.moin-restore` next to the live file (or, for a minute, has Litestream rebuild it there) and checks it. Then it stops the app, folds the live file's log into it, gives it a second name, `app.db.moin-before-<time>` (a hard link: the same file, no copy), and renames the restored file over `app.db` in one step. Then it starts the app. The stop lasts a moment.
- **Postgres.** The copy flows from your bucket straight into `pg_restore`, into a new database `moin_restore_<hash>` in the same Postgres, while your app keeps running; it gets the owner of the live database. Then moin stops the app's other services (the Postgres keeps running), renames `my-app` to `moin_before_<time>_<hash>` and the built one to `my-app` in one transaction, so both happen or neither, and starts them again. Settings and grants of the database itself (`ALTER DATABASE … SET`, `GRANT … ON DATABASE`) are not in a copy: set them again after a restore. Databases whose name starts with `moin_` are moin's own; name yours otherwise.

Both need room for one more database beside the live one: moin looks first. While a restore runs, edits through moin are refused (they would be lost, or lose it): make them when it is done. During a deploy they wait until the new version is up.

The undo stays as long as you keep copies, then moin removes it. Undo = the same switch the other way.

**If the server dies in the middle.** moin writes a small note, "restoring", before it changes anything. As the switch is one step, the database is whole either way: the old one or the restored one. When the daemon starts again it reads the note, looks which one is there (SQLite: is the undo another name of the live file, or a file of its own; Postgres: is there an undo), tidies what the restore left, starts the app and tells you how the restore ended. If it cannot look yet (a Postgres that does not answer), the note stays and the next start tries again.

## The daily check

Every day moin takes the newest copy from your bucket and restores it into a throwaway database on your server. SQLite: a temp file and SQLite's own `PRAGMA integrity_check`, which reads every page. Postgres: a throwaway database `moin_check_<hash>` where moin reads every row of every table, then drops it. So the check needs room for one more copy of the database for a while: next to the file for SQLite, in the volume of the Postgres. moin looks first, and the check fails with the numbers when there is not enough. The app shows when the check last ran, and an [alert](/docs/guides/alerts) goes out when it fails. "Backed up" means "copied, then restored and read".

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
                  └─ <generation>/
```

| Folder | What is in it |
| --- | --- |
| `<folder>/` | only when you set a **Folder** on the bucket |
| `<workspace>/` | your workspace's ID; it never changes |
| `<app>/` | the name the app was created with; it stays when you rename the app |
| `<app-id>/` | a new app with the same name gets a new one, so old copies never mix with new |
| `uploads/` | your code on its way to the server (`moin deploy`) |
| `<database>/` | one per database, its path with `~` for `/`: `data~app.db` for `/data/app.db`, `postgres-db` for the service `db`, `postgres-db~analytics` for another database in it |
| `copies/` | the copies, named by their time in UTC and six random digits, so two of one second stay apart: `20261107T140000Z-482913.db.gz` for SQLite, `….dump` for Postgres |
| `litestream/` | SQLite with Every change: every change, in generations. A restore starts a new one; the one before stays until it is older than you keep copies |

## How long things stay

The daemon deletes what is too old, once a minute after a copy and once an hour for uploads:

- **Copies** and **Every change**: as long as the app keeps copies (**Keep**). The newest copy always stays, so a database that stopped changing is never left without one.
- **The undo of a restore** (`app.db.moin-before-…` next to the file, `moin_before_…` in the same Postgres): as long as the app keeps copies.
- **Uploads**: until the server has them, a day at most.

moin never deletes the rest:

- **An app you removed** keeps its folder and its copies until you delete them, in your provider's dashboard or with any S3 tool.
- **Folders from an older moin** (`snapshots/`, `walg/`, `before-restore/`, `downloads/`) stay as they are. moin no longer reads them; delete them when you like.
- **A bucket you left** keeps what is in it.

## Several buckets

A workspace can keep several buckets: a client's data in the client's bucket, a bucket near the server, separate costs, production apart from tests. One is the **default**: every app without a bucket of its own, and every new app, uses it.

When an app moves to another bucket, its copies start again there. What is in the old bucket stays; moin deletes nothing.

## Under the hood

The daemon does it all, with one small set of verbs and one file per kind of database (`backup_sqlite.go`, `backup_postgres.go`); `backup.go` holds the schedule, the cleanup and the restore note:

| Verb | SQLite | Postgres |
| --- | --- | --- |
| **Copy** | `VACUUM INTO` a temp file, gzip, upload | `pg_dump -Fc` straight into the upload |
| **Restore** | build `app.db.moin-restore`, rename twice | `pg_restore` into `moin_restore_…`, rename twice |
| **Check** | `PRAGMA integrity_check` | read every table of `moin_check_…` |
| **Record** | Litestream, with Every change | |

Once a minute the daemon looks at each database: when the newest copy is older than **how often**, it takes one and deletes copies and undos older than **how long to keep**. When the newest copy is older than twice **how often**, and more than an hour past it (a big copy takes a while), the copies are late: the app says so and an [alert](/docs/guides/alerts) goes out. An app you stopped gets no alert that its database does not run.
