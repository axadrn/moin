---
title: "Roadmap"
description: "What ops does today, what comes next and what comes later"
order: 7
---

ops is in beta. It works and runs real sites today, and it keeps growing. No dates here: "next" means soon after launch, "later" means after that. Each item links to its GitHub issue, where you can follow it or say what you need.

## Now

What you get today, on every surface: the web, the CLI, the terminal UI, the API and MCP.

- **Deploys** from your repo, a GitHub branch or commit, or an image from CI. A failed deploy brings the last good one back. [Deploy](/docs/guides/deploy)
- **Backups** of every SQLite change into your own bucket, a snapshot before every change ops makes and a restore check every day. [Backups](/docs/guides/backups)
- **Data**: browse tables, edit rows and run SQL on the live database. [Data](/docs/guides/data)
- **Domains** with HTTPS, and a password in front if you like. [Domains](/docs/guides/domains)
- **Security**: a Cloudflare Tunnel instead of open ports, SSH only on Tailscale, a firewall and a rules file on your server. [Security](/docs/guides/security)
- **Teams and the activity log**: who changed what, to the second. [Activity log](/docs/guides/activity)
- **Self-hosting**, free, with every feature. [Self-hosting](/docs/self-hosting)

## Next

- **Uptime checks, built in**: every domain is watched from the outside from the first deploy on, nothing to set up. [#18](https://github.com/axadrn/ops/issues/18)
- **Alerts where your team is**: email, Slack, Discord, Telegram or a webhook, when an app goes down, a deploy fails or a server goes offline. [#19](https://github.com/axadrn/ops/issues/19)
- **Live metrics, built in**: CPU, RAM, disk and network for every server; requests, errors, response time and crashes for every app, with every deploy marked on the graphs. [#20](https://github.com/axadrn/ops/issues/20)
- **Env vars as one text**: switch between fields and a whole `.env` file you can paste. [#9](https://github.com/axadrn/ops/issues/9)
- **Vim keys in the terminal UI**: `gg`, `G`, `/` to search, `:` commands. [#11](https://github.com/axadrn/ops/issues/11)
- **List or grid, and your own order**: drag and drop apps and servers. [#14](https://github.com/axadrn/ops/issues/14)
- **More than one bucket**: pick per app where its uploads and backups go. [#15](https://github.com/axadrn/ops/issues/15)

## Later

Zero trust: even someone in full control of ops can't read your data or command your servers. [#16](https://github.com/axadrn/ops/issues/16)

- **Bucket keys only on your servers**: ops knows the bucket's name, never its key.
- **Secrets encrypted end to end**: only your servers can read your env vars.
- **Signed commands**: your server only obeys changes signed by a key on your own device.
- **Signed agent updates**: your server checks every new agent before it runs it.

Want it all in your own hands today? [Self-host ops](/docs/self-hosting): every feature, free, under AGPL.
