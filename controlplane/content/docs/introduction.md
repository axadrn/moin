---
title: "Introduction"
description: "What ops is and how its pieces fit"
order: 1
---

ops deploys your apps to servers you own, backs up every change of their SQLite database to your own bucket and lets you edit the data live. You use it from the browser, the `ops` CLI, the `ops` terminal UI and any AI agent through MCP. All four do the same things.

All of ops is open source. Use the hosted control plane, or [host it yourself for free](/docs/self-hosting): the same product, every feature, no limits.

## What it does

- **Deploys**: `ops deploy` from a repo, a branch or commit from [GitHub](/docs/guides/github), or an image from [CI](/docs/guides/ci). Health checks, and the last good release starts again when a deploy fails.
- **Backups**: every change of every SQLite database, continuously, into your bucket; a snapshot before every change; a restore drill every day. [Restore](/docs/guides/backups) a snapshot or any second of the last week.
- **Data**: browse tables, edit rows and run SQL on the live database. [Reads can never change a row](/docs/guides/data).
- **Domains**: HTTPS, www redirects and a password in front, in seconds. [Domains and access](/docs/guides/domains).
- **Security**: a Cloudflare Tunnel instead of open ports, SSH only on Tailscale, a firewall. [No open port](/docs/guides/security).

## The pieces

| Piece | What it is |
| --- | --- |
| **control plane** | This web app. It keeps your servers, apps, deploys and env vars, and tells agents what to do. Open source like the rest; we host it for you. |
| **agent** | `ops agent` on each server. It dials out to the control plane over one WebSocket. Nothing on your server listens for ops. |
| **`ops`** | One binary. `ops` opens the terminal UI, `ops <command>` is the CLI, `ops agent` is the agent. Open source under AGPL-3.0. |

## Words

- **server**: a machine with an agent.
- **app**: a Docker Compose project on one server.
- **deploy**: one release of an app, numbered per app: v1, v2, v3.
- **env vars**: settings and secrets of an app, stored encrypted.
- **backup**: a copy of an app's database in your bucket: continuous, or a snapshot.

## Where builds run

Builds run on **your** server. `ops deploy` puts the source into your bucket and the agent fetches it from there; a GitHub deploy goes from GitHub straight to your server. The control plane only relays messages: it never stores or runs your code, and your backups go from your server straight to your bucket.

Start with [Getting Started](/docs/getting-started).
