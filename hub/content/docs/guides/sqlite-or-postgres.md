---
title: "SQLite or Postgres?"
description: "We recommend SQLite; Postgres works fully; moin backs up both"
group: "Servers and data"
order: 6
---

We recommend SQLite. You do not have to use it. Postgres works fully.

moin backs up both, restores both and checks both every day.

| | How it is kept | How far back | Checked |
| --- | --- | --- | --- |
| **SQLite** | every change is written down | back to any minute | every day |
| **Postgres** | a copy on your schedule and before every deploy | back to any copy | every day |

## Why we recommend SQLite

- **Simpler.** Your database is one file next to your app. Nothing to run, nothing to update, no password.
- **No second box.** No database container that can stop or fill up.
- **Every change is backed up.** You can go back to any minute.

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

The one thing Postgres does not get is going back to any minute. It goes back to its copies. So pick a **How often** that fits, like every 15 minutes.

## Either way

Your backups go to your own bucket. A restore can be undone. [Backups](/docs/guides/backups) says how.
