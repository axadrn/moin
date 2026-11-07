---
title: "Roadmap"
description: "What moin does today, what comes next and what comes later"
group: "Reference"
order: 7
---

moin is in beta: it runs real sites today and keeps growing. No dates: "next" is soon after launch, "later" after that. Follow an item on its GitHub issue, or say what you need there.

## Now

On every surface: web, CLI, terminal UI, API and MCP.

- **Deploys** from your repo, a GitHub branch or commit, or an image from CI, with or without a Dockerfile; a failed deploy brings the last good one back. [Deploy](/docs/guides/deploy)
- **Backups** of every SQLite and Postgres database in your own bucket, on the schedule you pick: copies every 15 minutes, hourly or daily, kept 1 to 30 days; for SQLite every change too, to go back to any minute. Undo a restore, download a copy, and a check every day that a copy restores. [Backups](/docs/guides/backups)
- **Data**: browse, edit and query the live database. [Data](/docs/guides/data)
- **Domains** with HTTPS and a password, and an [instant address](/docs/guides/domains#instant-address) after the first deploy. [Domains](/docs/guides/domains)
- **Run a command** in an app's container, without SSH. [Run a command](/docs/guides/exec)
- **Catalog**: 47 apps such as Umami, n8n, Open WebUI and Vaultwarden in one click, updates with a backup first. [Catalog](/docs/guides/catalog)
- **Overview and ⌘K**: one page says whether everything is OK, `moin status` too. [Overview](/docs/guides/overview)
- **Uptime checks** of every domain once a minute, nothing to set up. [Uptime](/docs/guides/uptime)
- **Alerts** by mail, Slack, Discord, Telegram and webhooks. [Alerts](/docs/guides/alerts)
- **Metrics**: CPU, memory, disk and network of every server and app, up to 30 days back. [Metrics](/docs/guides/metrics)
- **More than one bucket**, one per app if you like. [Bucket](/docs/guides/bucket#more-than-one-bucket)
- **Updates in one click** for a self-hosted moin; agents update themselves. [Self-hosting](/docs/self-hosting#updates)
- **Env vars** as fields or one `.env` text. [Env vars](/docs/guides/env)
- **Terminal UI** for the daily work. [Terminal UI](/docs/reference/tui)
- **Security**: Cloudflare Tunnel, SSH only on Tailscale, a firewall and a rules file on your server. [Security](/docs/guides/security)
- **Team and activity**: who changed what, to the second. [Activity](/docs/guides/activity)
- **Self-hosting**, free, every feature, one command. [Self-hosting](/docs/self-hosting)

## Next

- **Servers in one click** at Hetzner, DigitalOcean, Vultr and UpCloud, right from moin. [#1](https://github.com/axadrn/moin/issues/1)
- **MySQL backups**, copies on your schedule and a daily check, like Postgres. MongoDB, ClickHouse and the rest run like any service; their backups come when you ask for them.

## Later

Zero trust: even someone in full control of moin can't read your data or command your servers. [#16](https://github.com/axadrn/moin/issues/16)

- **Bucket keys only on your servers**: moin knows the bucket's name, never its key.
- **Secrets encrypted end to end**: only your servers can read your env vars.
- **Signed commands**: your server only obeys changes signed by a key on your own device.
- **Signed agent updates**: your server checks every new agent before it runs it.
