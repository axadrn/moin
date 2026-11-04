---
title: "Roadmap"
description: "What ops does today, what comes next and what comes later"
order: 7
---

ops is in beta. It works and runs real sites today, and it keeps growing. No dates here: "next" means soon after launch, "later" means after that. Each item links to its GitHub issue, where you can follow it or say what you need.

## Now

What you get today, on every surface: the web, the CLI, the terminal UI, the API and MCP.

- **Deploys** from your repo, a GitHub branch or commit, or an image from CI. A failed deploy brings the last good one back. [Deploy](/docs/guides/deploy)
- **Backups** of every SQLite and Postgres change into your own bucket, a full copy before every change ops makes and a restore check every day. [Backups](/docs/guides/backups)
- **Data**: browse tables, edit rows and run SQL on the live database, SQLite or Postgres. [Data](/docs/guides/data)
- **Domains** with HTTPS, and a password in front if you like. [Domains](/docs/guides/domains)
- **App catalog**: Umami, n8n, Vaultwarden, Gitea and more in one click, every release the hour it ships, updates with a snapshot first. [Catalog](/docs/guides/catalog)
- **Uptime checks, built in**: every domain checked from the outside once a minute from the first deploy on, nothing to set up; down and back up in the activity log. [Uptime](/docs/guides/uptime)
- **Alerts by mail, Slack, Discord, Telegram and webhooks**: when a site goes down or comes back, a server goes offline or a deploy fails, with a test for every destination and signed webhooks. [Alerts](/docs/guides/alerts)
- **Live metrics, built in**: CPU, memory, disk and network for every server and app, live for the last hour and up to 30 days back, with every deploy marked on the charts. [Metrics](/docs/guides/metrics)
- **More than one bucket**: give each app its own bucket for uploads and backups, say one per client. [Bucket](/docs/guides/bucket)
- **Updates in one click**: a self-hosted ops tells you when a release is out and updates itself; agents update themselves. [Self-hosting](/docs/self-hosting#updates)
- **Env vars as one text**: switch between fields and a whole `.env` file. [Env vars](/docs/guides/env)
- **Your own order, list or grid**: drag apps and servers where you want them. [Servers](/docs/guides/servers#order-your-servers)
- **Vim keys in the terminal UI**: `gg`, `G`, `/` to search, `:` commands. [Terminal UI](/docs/reference/tui)
- **Security**: a Cloudflare Tunnel instead of open ports, SSH only on Tailscale, a firewall and a rules file on your server. [Security](/docs/guides/security)
- **Teams and the activity log**: who changed what, to the second. [Activity log](/docs/guides/activity)
- **Self-hosting**, free, with every feature. [Self-hosting](/docs/self-hosting)

## Next

- **Servers in one click**: create them at Hetzner, DigitalOcean, Vultr and UpCloud, right from ops. [#1](https://github.com/axadrn/moin/issues/1)

- **MySQL backups**: every change backed up and checked daily, like Postgres, so WordPress and the other MySQL apps of the catalog lose nothing either.

MongoDB, ClickHouse and the rest run like any service; ops backups for them come when you ask for them.

## Later

Zero trust: even someone in full control of ops can't read your data or command your servers. [#16](https://github.com/axadrn/moin/issues/16)

- **Bucket keys only on your servers**: ops knows the bucket's name, never its key.
- **Secrets encrypted end to end**: only your servers can read your env vars.
- **Signed commands**: your server only obeys changes signed by a key on your own device.
- **Signed agent updates**: your server checks every new agent before it runs it.

Want it all in your own hands today? [Self-host ops](/docs/self-hosting): every feature, free, under AGPL.
