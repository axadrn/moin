---
title: "Overview"
description: "Is everything OK? See it at a glance. Search to jump anywhere."
group: "Every day"
order: 1
---

The Overview answers one question: is everything OK? It says **Everything runs.** (`moin status` says `All good.`) Or it lists the few things that need you, each with a link to fix it.

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

On your first visit, with no server yet, it shows the three steps to a live app: connect a server, deploy your app, and moin keeps it safe. [No server yet?](/docs/guides/servers#no-server-yet)

## What needs you

The most urgent first:

- **A server offline**: its daemon stopped answering.
- **A failed deploy**, with the version that still runs.
- **A missing registry login**: the app's server has none for the registry of its image, so its next deploy stops ([Registry logins](/docs/guides/ci#registry-logins)).
- **Backups that stopped**, and **a daily check that failed** ([Backups](/docs/guides/backups)).
- **A domain down** ([Uptime](/docs/guides/uptime)).
- **A disk over 90%** on a server ([Metrics](/docs/guides/metrics)).
- **A daemon behind** the hub ([Servers](/docs/guides/servers#updates)).

Below that: your pinned apps, the apps deployed last, and the newest catalog updates.

## Jump anywhere

`⌘K`, `ctrl+k`, `/` or the search in the bar opens the palette. Type a few letters, then `enter`. [Palette and keys](/docs/reference/palette).

## The bar and Settings

The bar on every page holds the wordmark, the search and your picture. Your picture opens **Settings**: the workspace's **Team**, **Alerts**, **Bucket**, **Registries**, **Integrations**, **Activity** and **Billing**, your workspaces, your **Account** and your **Connections**.

## Good to know

- The API answers `ok`, the `issues`, the `updates` and the `counts`.
- A backup counts as checked when its copies come and the newest one restored in the last day. Backups that stopped never count.
