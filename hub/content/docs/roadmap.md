---
title: "Roadmap"
description: "What moin does today, what comes soon, what comes later, and moin next"
group: "Reference"
order: 7
---

moin is in beta. It runs real sites today. There are no dates: "soon" is shortly after launch, "later" after that.

## Now

On every surface: web, CLI, terminal UI, API and MCP.

- **Deploys** from GitHub or CI, with or without a Dockerfile. A failed deploy brings the last good one back. [Deploy](/docs/guides/deploy)
- **Your compose file**: see and download the one moin runs, to run your app anywhere. [Compose](/docs/guides/compose#the-compose-file-moin-runs)
- **Backups** of every SQLite and Postgres database in your own bucket, on the schedule you pick. A check every day that a copy restores. [Backups](/docs/guides/backups)
- **Data**: browse, edit and query the live database. [Data](/docs/guides/data)
- **Domains** with HTTPS and a password, and an [instant address](/docs/guides/domains#instant-address) after the first deploy. [Domains](/docs/guides/domains)
- **Run a command** in an app's container, without SSH. [Run a command](/docs/guides/exec)
- **Catalog**: 47 apps such as Umami, n8n, Open WebUI and Vaultwarden in one click, updates with a backup first. [Catalog](/docs/guides/catalog)
- **Overview and ⌘K**: one page says whether everything is OK, `moin status` too. [Overview](/docs/guides/overview)
- **Uptime** of every domain once a minute, nothing to set up. [Uptime](/docs/guides/uptime)
- **Alerts** by mail, Slack, Discord, Telegram and webhooks. [Alerts](/docs/guides/alerts)
- **Metrics**: CPU, memory, disk and network of every server and app, up to 30 days back. [Metrics](/docs/guides/metrics)
- **More than one bucket**, one per app if you like. [Bucket](/docs/guides/bucket#more-than-one-bucket)
- **Updates in one click** for a self-hosted moin; daemons update themselves. [Self-hosting](/docs/self-hosting#updates)
- **Env vars** as fields or one `.env` text, kept only on your server. [Env vars](/docs/guides/env)
- **Terminal UI** for the daily work. [Terminal UI](/docs/reference/tui)
- **Security**: Cloudflare Tunnel, SSH only on Tailscale, a firewall and a rules file on your server the hub cannot change. Signed daemon updates. Every request of the hub in the server's journal. Env values, bucket keys and registry logins live only there. [Security](/docs/guides/security)
- **Sign-in** with GitHub or a passkey. The CLI, the TUI and AI agents connect in the browser, each with its own rights; AI agents through OAuth. CI gets a CI token. [Account](/docs/guides/account)
- **Team and activity**: who changed what, to the second. [Activity](/docs/guides/activity)
- **Self-hosting**, free, every feature, one command. Its GitHub App in one click. [Self-hosting](/docs/self-hosting)

## Soon

- **Servers in one click** at Hetzner, DigitalOcean, Vultr and UpCloud, right from moin. [#1](https://github.com/axadrn/moin/issues/1)
- **MySQL backups**, like Postgres. MongoDB and the rest run like any service, but moin cannot back them up yet. They show in red.

## Later

Zero trust: even someone in full control of moin can't read your data or command your servers. Today the rules on your server narrow what such a person can do, but not to nothing: [If someone breaks into the hub](/docs/guides/security#if-someone-breaks-into-the-hub). [#16](https://github.com/axadrn/moin/issues/16)

- **Signed commands**: your server only obeys changes signed by a key on your own device.

## moin next

Deploying stays the main thing. [moin next](/next) is what grows next to it: the other tools a team needs to build software, in the same app.

Highly experimental: it can break, change or vanish any day. Each one starts with a few users behind a switch, and runs on its own, so a bug in one never touches another or your deploys. What is ready for everyone leaves this list and is simply part of moin. We shape it together, in the open: [open an issue](https://github.com/axadrn/moin/issues).

- **Tasks and roadmaps**: what to do, who does it, and what comes after.
- **Docs**: what your team knows, next to the work.
- **Chat**: talk where the work is.
- **Mail**: your team's mail, on your own domain.
- **Analytics**: who uses what you shipped.
- **And many more.**
