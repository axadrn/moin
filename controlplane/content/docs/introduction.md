---
title: "Introduction"
description: "What ops is and how its pieces fit"
order: 1
---

ops deploys your apps to servers you own, backs up every change of their SQLite database to your own bucket and lets you edit the data live. You use it from the browser, the `ops` CLI, the `ops` terminal UI and any AI agent through MCP. All four do the same things.

All of ops is open source. Use the hosted control plane, or [host it yourself for free](/docs/self-hosting): the same product, every feature, no limits.

ops is in beta: it works and runs real sites, and things may still change. When something you use has to change, we tell you before it breaks.

## What it does

- **Deploys**: `ops deploy` from a repo, a branch or commit from [GitHub](/docs/guides/github), or an image from [CI](/docs/guides/ci). Health checks, and the last good release starts again when a deploy fails.
- **Backups**: every change of every SQLite database, continuously, into your bucket; a snapshot before every change; a restore drill every day. [Restore](/docs/guides/backups) a snapshot or any second of the last week.
- **Data**: browse tables, edit rows and run SQL on the live database. [Reads can never change a row](/docs/guides/data).
- **Domains**: HTTPS, www redirects and a password in front, in seconds. [Domains and access](/docs/guides/domains).
- **Security**: a Cloudflare Tunnel instead of open ports, SSH only on Tailscale, a firewall. [No open port](/docs/guides/security).
- **Activity log**: who changed what, to the second, from the web, the CLI, an agent or GitHub. Never your secrets or your data. [See what it records](/docs/guides/activity).

## The pieces

| Piece | What it is |
| --- | --- |
| **control plane** | This web app. It keeps your servers, apps, deploys and env vars, and tells agents what to do. Open source like the rest; we host it for you. |
| **agent** | `ops agent` on each server. It dials out to the control plane over one WebSocket. Nothing on your server listens for ops. |
| **`ops`** | One binary. `ops` opens the terminal UI, `ops <command>` is the CLI, `ops agent` is the agent. Open source under AGPL-3.0. |

## How ops works in 2 minutes

- **workspace**: your team's space in ops. It holds your servers, apps, bucket and people. You pay per workspace.
- **server**: a computer you rent, for example at Hetzner, with the ops agent on it.
- **agent**: a small program on your server. It calls ops and does the work there, like a caretaker who phones in for the day's jobs. Nothing on your server waits for calls from ops.
- **app**: one website or service, such as your shop. It is a Docker Compose project and lives on one server.
- **deploy**: one release of an app, numbered v1, v2, v3. When a new one fails, the last good one starts again.
- **env vars**: the settings and secrets of an app, such as an API key. ops keeps them encrypted and hands them to your server with each deploy.
- **domain**: the address people type to reach your app, such as shop.example.com. ops gives it HTTPS.
- **bucket**: your own storage box, for example at Cloudflare R2. One per workspace. It holds your code on its way to the server and your backups. ops never stores either.
- **backup**: every change of your app's SQLite database, copied to your bucket about once a second.
- **snapshot**: a full copy of the database at one moment, like a photo. ops takes one before every deploy, restore and data change.
- **restore**: puts the database back to a snapshot, or to any second of the last 7 days.
- **team members and roles**: the people in your workspace. Members deploy and fix things, admins also invite people, the owner also handles billing and roles.
- **API token**: a key for the CLI, scripts and AI agents to act for you. It belongs to one workspace and can read, or read and write.
- **activity log**: the list of who changed what in your workspace, and when.

```
workspace "acme"     your team, your bucket and your servers
├── server web-1     app shop, app blog
└── server web-2     app api

ops deploy   laptop ──▶ your bucket ──▶ web-1     (your code, gone after the deploy)
GitHub       GitHub ──────────────────▶ web-2     (no bucket needed)
backups      web-1, web-2 ──▶ your bucket          (every change, plus snapshots)
```

## Where builds run

Builds run on **your** server. `ops deploy` puts the source into your bucket and the agent fetches it from there; a GitHub deploy goes from GitHub straight to your server. The control plane only relays messages: it never stores or runs your code, and your backups go from your server straight to your bucket.

Start with [Getting Started](/docs/getting-started).
