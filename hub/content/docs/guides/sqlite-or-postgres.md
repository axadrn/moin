---
title: "SQLite or Postgres?"
description: "We recommend SQLite. Postgres works fully. moin backs up both."
group: "Servers and data"
order: 6
---

We recommend SQLite. You do not have to use it. Postgres works fully.

moin backs up both, restores both and checks both every day.

## Why we recommend SQLite

- **Simpler.** Your database is one file next to your app. Nothing to run, nothing to update, no password.
- **No second box.** No database container that can stop or fill up.

```
SQLite        Postgres
┌────────┐    ┌────────┐
│ app    │    │ app    │
│ app.db │    └───┬────┘
└────────┘    ┌───▼────┐
one box       │ db     │
              └────────┘
              two boxes
```

## When Postgres

- Your app or framework needs it, like many catalog apps do.
- Several apps or servers write to one database.
- You need what only Postgres has, such as PostGIS or pgvector.

## Either way

Your copies go to your own bucket, on the **How often** you pick. Every 15 minutes is the shortest. A restore can be undone. [Backups](/docs/guides/backups) says how.
