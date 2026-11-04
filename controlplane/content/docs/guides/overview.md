---
title: "Overview"
description: "Whether everything is OK, at a glance, and ⌘K to jump anywhere"
order: 0
---

**Is everything OK?** The Overview answers that first. It is the first page of a workspace: open the panel and you see either **All good.** with what runs, or the few things that need you, each with a link to where you fix it.

## What needs you

ops lists only what you have to act on, the most urgent first:

- **A server offline**: its agent stopped answering. A server whose agent never connected is not on the list.
- **A failed deploy**: the newest deploy of an app failed, with the version that still runs.
- **Backups that stopped**, and **a restore check that failed** ([Backups](/docs/guides/backups)).
- **A domain down**, from the [uptime](/docs/guides/uptime) checks.
- **A disk over 90%** on a server ([Metrics](/docs/guides/metrics)).
- **An agent behind** the control plane, which updates by itself or with `ops servers update` ([Servers](/docs/guides/servers)).

Below it: the counts (apps live, servers online, backups checked in the last 24 hours), the [catalog](/docs/guides/catalog) apps with a newer release, and the last changes from the [activity log](/docs/guides/activity). Nothing new is stored; the Overview reads what ops already knows.

```surfaces
Web: **Overview**, the first item of the sidebar, or `g` then `o`
CLI: ops status
TUI: :status
API: GET /api/v1/status
MCP: get_status
```

`ops status` exits with `1` when something needs you and `0` when all is good, so a script or a cron job can check it:

```bash
ops status || echo "ops needs you"
```

The API answers `ok`, the `issues` with their `kind`, `target`, `detail` and the `path` in the panel where you fix them, the `updates` and the `counts`.

## Jump anywhere

`⌘K`, or `Ctrl+K`, opens a search over the whole panel: every page, every app, server and bucket. Type a few letters, then `enter`. The apps, servers and buckets load on the first open, so it stays fast with hundreds of apps.

It also runs the everyday actions of an app: **Deploy**, **Restart** and **Roll back**, each asking first, like the buttons on the app.

## Keyboard shortcuts

`?` shows every key of the panel, anywhere unless you are typing.

| Key | Does |
| --- | --- |
| `⌘K` · `Ctrl+K` | jump to an app, a server or a page |
| `?` | the list of keys |
| `g` then `o` · `a` · `s` · `b` · `y` · `t` · `l` · `,` | Overview, Apps, Servers, Buckets, Activity, Team, Alerts, Settings |
| `/` | search, or the page's main field |
| `esc` | close, or leave a field |
