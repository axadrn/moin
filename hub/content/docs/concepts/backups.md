---
title: "How backups work"
description: "Every change of SQLite, copies of Postgres, a daily check, and what lives where"
group: "Servers and data"
order: 4
---

This page says how backups work inside. To use them, read [Backups](/docs/guides/backups).

| | How it is kept | How far back | Checked |
| --- | --- | --- | --- |
| **SQLite** | every change is written down | back to any minute | every day |
| **Postgres** | a copy on your schedule and before every deploy | back to any copy | every day |

## Where the work happens

On your server. The daemon writes the backups to your bucket. Your data never goes through moin. The hub only keeps your settings and shows you the list.

```
hub
  keeps: how long, how often
  shows: the list
  holds: no data

your server ──backups──▶ your bucket
```

## SQLite: every change

[Litestream](https://litestream.io), a small tool inside the daemon, sends every change to your bucket within about a second.

It keeps the changes as long as the app keeps its backups (**Keep**). moin never copies the live file itself.

After a deploy, Litestream starts over: moin lets go of the app's SQLite files and follows them again on the next round. The history stays.

A deploy or a data edit takes no copy. moin only remembers the time. **Before v12** is that time in the history.

## Postgres: copies

```
pg_dump ─▶ straight into your bucket
           (nothing waits on the disk)
```

`pg_dump` makes a clean copy while your app keeps running. It writes a stream, like water from a tap. moin points the tap straight at your bucket. It runs inside your Postgres container, so no password leaves it. It copies every database in it, each into a file of its own.

A copy runs as long as data moves. It stops only after 30 minutes with no data.

Each copy notes the size of the database. A restore and the daily check read that note to see if there is room.

## A restore

A restore never changes your data in place. moin builds the database beside the live one. It switches in one step. It keeps the old one as the undo. SQLite restores with Litestream, to the minute you picked.

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

Both need room for one more database. moin looks first.

While a restore runs, edits through moin are refused. During a deploy they wait until the new version is up.

**If the server dies in the middle.** moin writes a small note before it changes anything. The switch is one step, so the database is whole either way. When the daemon starts again it reads the note, tidies up, starts the app and tells you how it ended.

## The daily check

Every day moin restores the newest state into a throwaway database on your server: SQLite with Litestream, Postgres from its newest copy. It reads every page or every row. Then it drops it.

So "backed up" means "written down, then restored and read". The check needs room for one more database for a while. If it fails, you get an [alert](/docs/guides/alerts). If Litestream stops writing, you get one too.

## What lives where

```
<your bucket>/
└─ <folder>/
   └─ <workspace>/
      └─ <app>/
         └─ <app-id>/
            ├─ uploads/
            ├─ downloads/
            └─ <database>/
               ├─ copies/
               └─ litestream/
                  └─ <generation>/
```

| Folder | What is in it |
| --- | --- |
| `<folder>/` | only when you set a **Folder** on the bucket |
| `<workspace>/` | your workspace's ID, so two workspaces can share a bucket |
| `<app>/` | the name the app was created with; it stays when you rename the app |
| `<app-id>/` | a new app with the same name gets a new one, so copies never mix |
| `uploads/` | your code on its way to the server |
| `downloads/` | SQLite downloads, ready for the link |
| `<database>/` | one per database: `data~app.db` for `/data/app.db`, `postgres-db` for the service `db`, `postgres-db~analytics` for another database in it |
| `copies/` | Postgres: the copies, named by their time in UTC |
| `litestream/` | SQLite: every change, in generations |

## How long things stay

- **Every change** and **copies**: as long as the app keeps them (**Keep**). The newest Postgres copy always stays.
- **The undo of a restore**: as long as the app keeps its backups.
- **Uploads** and **SQLite downloads**: a day at most.

moin never deletes the rest:

- **An app you removed** keeps its copies until you delete them, in your provider's dashboard or with any S3 tool.
- **A bucket you left** keeps what is in it.
- **Folders from an older moin** (`snapshots/`, `walg/`, `before-restore/`) stay. moin no longer reads them.

## Several buckets

A workspace can keep several buckets. One is the **default**: every app without a bucket of its own uses it. When an app moves to another bucket, its backups start again there. [Bucket](/docs/guides/bucket#more-than-one-bucket).

## When Postgres copies are late

Once a minute the daemon looks at each Postgres database. When the newest copy is older than **how often**, it takes one.

When the newest copy is older than twice **how often**, and more than an hour past it, the copies are late. The app says so and an [alert](/docs/guides/alerts) goes out.
