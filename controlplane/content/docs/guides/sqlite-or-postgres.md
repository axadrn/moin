---
title: "SQLite or Postgres?"
description: "Pick SQLite unless you need Postgres; moin backs up both"
group: "Servers and data"
order: 6
---

Pick SQLite. Take Postgres when your app needs it. moin backs up both.

## Why SQLite

- **Simpler.** Your database is one file next to your app. Nothing to run, nothing to update, no password.
- **No second server.** No database container that can stop, fill up or need more memory.
- **The most used database.** It runs in every phone and every browser. It is old, tested and fast for one server.
- **Every change is backed up.** With moin, **Every change** writes down each change in your bucket, so you can go back to any minute, not only to the last copy.

```
SQLite                Postgres
┌────────┐            ┌────────┐
│ app    │            │ app    │
│ app.db │            └───┬────┘
└────────┘            ┌───▼────┐
one box               │ db     │
                      └────────┘
                      two boxes
```

## When Postgres

- Your app or framework needs it, like many catalog apps do.
- Several apps, or several servers, write to one database.
- You need what only Postgres has, such as PostGIS or pgvector.

Postgres stays fully supported. moin copies it on your schedule with `pg_dump`, restores and undoes the same way, and checks it every day. The one thing it does not get is going back to any minute: it goes back to its copies, so pick **How often** to fit, like every 15 minutes.

## Either way

Your copies go to your own bucket, a restore can be undone, and every day moin checks that a copy restores. [Backups](/docs/guides/backups) says how.
