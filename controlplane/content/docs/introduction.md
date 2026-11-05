---
title: "Introduction"
description: "What moin is and how its pieces fit"
order: 1
---

Deploy anything. Lose nothing. moin deploys your apps to your servers, backs up every change of their SQLite or Postgres database to your own bucket, tests a restore every day and lets you edit the data live. You use it from the browser, the `moin` CLI (commands you type), the `moin` terminal UI (a screen in your terminal you drive with keys) and any AI agent through MCP. All four do the same work, and so does the [API](/docs/reference/api) for your own scripts. Your account and the roles of your team change only on the web.

New here? Read [How moin works in 2 minutes](#how-moin-works-in-2-minutes) below, then [Getting Started](/docs/getting-started).

All of moin is open source. Use the hosted control plane, or [host it yourself for free](/docs/self-hosting): the same product, every feature, no limits.

moin is in beta: it works and runs real sites, and it keeps getting better fast, so things may change.

## What it does

- **Deploys**: `moin deploy` from a repo, a branch or commit from [GitHub](/docs/guides/github), or an image from [CI](/docs/guides/ci). No Dockerfile needed: moin builds Node, Bun, Python, Ruby, PHP, Go and more. Health checks, and the last good release starts again when a deploy fails. [Deploy](/docs/guides/deploy).
- **Catalog**: 47 open-source apps in one click, such as n8n, Umami and Open WebUI, with an alert when a new release is out and a snapshot before each update. [Catalog](/docs/guides/catalog).
- **Backups**: every change of every SQLite and Postgres database, continuously, into your bucket; a full copy before every change; a restore check every day. [Restore](/docs/guides/backups) a snapshot or any second of the last week.
- **Data**: browse tables, edit rows and run SQL on the live database, SQLite or Postgres. [Reads can never change a row](/docs/guides/data).
- **Domains**: every app online at once at an [instant address](/docs/guides/domains#instant-address) with HTTPS and a password, then your own domains, one per service if you like. [Domains and access](/docs/guides/domains).
- **Run a command** in an app's container, without SSH: migrations, the first admin. [Run a command](/docs/guides/exec).
- **Watch**: [uptime checks](/docs/guides/uptime) every minute, [metrics](/docs/guides/metrics), [alerts](/docs/guides/alerts) by email, Slack, Discord, Telegram or webhook, and an [overview](/docs/guides/overview) that says whether everything is OK.
- **Security**: a Cloudflare Tunnel instead of open ports, SSH only on Tailscale, a firewall. [No open port](/docs/guides/security).
- **Activity log**: who changed what, to the second, from the web, the CLI, an agent or GitHub. Never your secrets or your data. [See what it records](/docs/guides/activity).

## The pieces

| Piece | What it is |
| --- | --- |
| **control plane** | This web app. It keeps your servers, apps, deploys and env vars, and tells agents what to do. Open source like the rest; we host it for you. |
| **agent** | `moin agent` on each server. It opens one connection to the control plane and keeps it open. Nothing on your server listens for moin. |
| **`moin`** | One binary. `moin` opens the terminal UI, `moin <command>` is the CLI, `moin agent` is the agent. Open source under AGPL-3.0. |

## How moin works in 2 minutes

- **control plane**: the moin website you sign in to. It keeps the list of your servers and apps and tells them what to do.
- **workspace**: your team's space in moin. It holds your servers, apps, bucket and people. You get one when you sign up. On hosted moin you pay per workspace.
- **server**: a computer you rent, for example at Hetzner, with the moin agent on it.
- **agent**: a small program on your server. It calls moin and does the work there, like a caretaker who phones in for the day's jobs. Nothing on your server waits for calls from moin.
- **app**: one website or service, such as your shop. It lives on one server.
- **Docker** and **Docker Compose**: the tools that run your app in containers on the server. A container is a sealed box with your app and everything it needs. A `compose.yml` file says which containers an app has. You do not need to know Docker: for a `Dockerfile` or a Go module, moin writes the compose file for you.
- **deploy**: one release of an app, numbered v1, v2, v3. When a new one fails, the last good one starts again.
- **health check**: after a deploy, moin checks that the app starts and stays up, and can call a web address of the app, such as `/healthz`. If the check fails, the last good release starts again.
- **env vars**: the settings and secrets of an app, such as an API key. moin keeps them encrypted and hands them to your server with each deploy.
- **domain**: the address people type to reach your app, such as shop.example.com. moin gives it HTTPS.
- **bucket**: your own storage box, for example at Cloudflare R2. One per workspace, or one per client or region: each app picks one, the default serves the rest. It holds your code on its way to the server and your backups. moin never stores either.
- **SQLite**: a database that lives in one file next to your app, such as `/data/app.db`. moin finds it, backs it up and lets you edit it.
- **Postgres**: a database server that runs as a service of your app. moin finds it in your compose.yml, backs it up and lets you edit it, like SQLite.
- **backup**: every change of your app's database, copied to your bucket: about once a second for SQLite, within a minute for Postgres.
- **snapshot**: a full copy of the database at one moment, like a photo. moin takes one before every deploy, restore and data change.
- **restore**: puts the database back to a snapshot, or to any second of the last 7 days.
- **team members and roles**: the people in your workspace. Members deploy and fix things. Admins also invite people and change their roles. The owner also pays and decides who else is an owner.
- **API token**: a key for the CLI, scripts and AI agents to act for you. It belongs to one workspace and can read, or read and write.
- **MCP**: the way AI agents such as Claude or Cursor use tools. moin offers almost every action as an MCP tool. [AI agents (MCP)](/docs/guides/mcp).
- **activity log**: the list of who changed what in your workspace, and when.

```
workspace            your team, your bucket and your servers
├── server web-1     app shop, app blog
└── server web-2     app api

moin deploy   laptop ──▶ your bucket ──▶ web-1     (your code, gone after the deploy)
GitHub       GitHub ──────────────────▶ web-2     (no bucket needed)
backups      web-1, web-2 ──▶ your bucket          (every change, plus snapshots)
```

## Where builds run

Builds run on **your** server. `moin deploy` puts the source into your bucket and the agent fetches it from there; a GitHub deploy goes from GitHub straight to your server. The control plane only relays messages: it never stores or runs your code, and your backups go from your server straight to your bucket.

Start with [Getting Started](/docs/getting-started).

Why the name? moin is how Hamburg says hello: short, friendly, at any hour. Hamburg is a container port, and your apps run in containers too: moin ships them to your servers and loses none of their data.
