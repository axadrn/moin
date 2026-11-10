---
title: "How backups work"
description: "Copies on your schedule. In your own bucket. A daily check. What lives where."
group: "Servers and data"
order: 4
---

This page says how backups work inside. To use them, read [Backups](/docs/guides/backups).

## Where the work happens

On your server. The daemon makes each copy and puts it in your bucket. Your data never goes through moin. The hub only keeps your settings and shows you the list.

```
hub
  keeps: how often, how long
  shows: the list
  holds: no data

your server ──copies──▶ your bucket
```

## How a copy is made

```
SQLite: app.db
  ─▶ temp copy (VACUUM INTO)
  ─▶ gzip ─▶ your bucket
  ─▶ temp copy deleted at once

Postgres: pg_dump
  ─▶ straight into your bucket
  (nothing waits on the disk)
```

**SQLite.** `VACUUM INTO` makes a clean copy while your app keeps running. It can only write into a file, like a photocopier that needs paper. So the copy is a file next to the database for a moment, and the disk needs room for it. moin looks first.

**Postgres.** `pg_dump` makes a clean copy while your app keeps running. It writes a stream, like water from a tap. moin points the tap straight at your bucket. It runs inside your Postgres container, so no password leaves it. It copies every database in it, each into a file of its own.

A copy runs as long as data moves. It stops only after 30 minutes with no data.

Each copy notes the size of the database. A restore and the daily check read that note to see if there is room.

## A restore

A restore never changes your data in place. moin builds the database beside the live one. It switches in one step. It keeps the old one as the undo.

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

Postgres works the same way, with databases instead of files. Both renames happen in one transaction.

```
1 build beside, the app runs
  my-app           live
  moin_restore_…   built

2 stop the app, switch
  my-app ─▶ moin_before_…
  moin_restore_… ─▶ my-app

3 start the app
  my-app           restored
  moin_before_…    the undo
```

Settings of the database itself (`ALTER DATABASE … SET`, `GRANT … ON DATABASE`) are not in a copy. Set them again after a restore. Databases whose name starts with `moin_` are moin's own.

Both need room for one more database. moin looks first, using the size noted on the copy.

While a restore runs, edits through moin are refused. During a deploy they wait until the new version is up.

**If the server dies in the middle.** moin writes a small note before it changes anything. The switch is one step, so the database is whole either way. When the daemon starts again it reads the note, tidies up, starts the app and tells you how it ended.

## The daily check

Every day moin takes the newest copy and restores it into a throwaway database on your server. It reads every page or every row. Then it drops it.

So "backed up" means "copied, then restored and read". The check needs room for one more copy of the database for a while. If it fails, the database shows red and you get an [alert](/docs/guides/alerts).

## What lives where

```
<your bucket>/
└─ <folder>/
   └─ <workspace>/
      └─ <app>/
         └─ <app-id>/
            ├─ uploads/
            └─ <database>/
               └─ copies/
```

| Folder | What is in it |
| --- | --- |
| `<folder>/` | only when you set a **Folder** on the bucket |
| `<workspace>/` | your workspace's ID, so two workspaces can share a bucket |
| `<app>/` | the name the app was created with; it stays when you rename the app |
| `<app-id>/` | a new app with the same name gets a new one, so copies never mix |
| `uploads/` | your code on its way to the server |
| `<database>/` | one per database: `data~app.db` for `/data/app.db`, `postgres-db` for the service `db`, `postgres-db~analytics` for another database in it |
| `copies/` | the copies, named by their time in UTC |

## How long things stay

- **Copies**: as long as the app keeps copies (**Keep**). The newest copy always stays.
- **The undo of a restore**: as long as the app keeps copies.
- **Uploads**: until the server has them, a day at most.

moin never deletes the rest:

- **An app you removed** keeps its copies until you delete them, in your provider's dashboard or with any S3 tool.
- **A bucket you left** keeps what is in it.
- **Folders from an older moin** (`snapshots/`, `walg/`, `before-restore/`, `downloads/`, `litestream/`) stay. moin no longer reads them.

## Several buckets

A workspace can keep several buckets. One is the **default**: every app without a bucket of its own uses it. When an app moves to another bucket, its copies start again there. [Bucket](/docs/guides/bucket#more-than-one-bucket).

## When copies are late

Once a minute the daemon looks at each database. When the newest copy is older than **how often**, it takes one.

When the newest copy is older than twice **how often**, and more than an hour past it, the copies are late. The app says so and an [alert](/docs/guides/alerts) goes out.
