---
title: "Overview"
description: "Whether everything is OK, at a glance, and the search to jump anywhere"
group: "Every day"
order: 1
---

The Overview answers one question: is everything OK? It says **All good.**, or lists the few things that need you, each with a link to fix it.

```surfaces
Web: **Overview**, the first page of a workspace, or `g` then `o`
CLI: moin status
TUI: the overview, where `moin` starts
API: GET /api/v1/status
MCP: get_status
```

`moin status` exits with `1` when something needs you. So a script can check it:

```bash
moin status || echo "moin needs you"
```

## What needs you

The most urgent first:

- **A server offline**: its daemon stopped answering.
- **A failed deploy**, with the version that still runs.
- **Backups that stopped**, and **a daily check that failed** ([Backups](/docs/guides/backups)).
- **A domain down** ([Uptime](/docs/guides/uptime)).
- **A disk over 90%** on a server ([Metrics](/docs/guides/metrics)).
- **A daemon behind** the hub ([Servers](/docs/guides/servers#updates)).

Below that: what runs, the newest catalog updates, and the newest [activity](/docs/guides/activity).

## Jump anywhere

`⌘K`, `ctrl+k`, `/` or the search in the bar opens the palette. Type a few letters, then `enter`. [Palette and keys](/docs/reference/palette).

## The bar and Settings

The bar on every page holds the wordmark, the search and your picture. Your picture opens **Settings**: the workspace's **Team**, **GitHub**, **Alerts**, **Bucket**, **Activity** and **Billing**, your workspaces, and your **Account**.

## Good to know

- The API answers `ok`, the `issues`, the `updates` and the `counts`.
