---
title: "Roadmap"
description: "What moin does today. What comes next. What comes later. No dates."
group: "Reference"
order: 5
---

moin is in beta. It runs real sites today. There are no dates.

## Now

- **Deploy** over SSH, with your key. From your folder, an image from your registry or your laptop, or the catalog. A failed deploy brings the last good one back. [Deploy](/docs/deploy)
- **Backups** of every SQLite, Postgres, MySQL, MariaDB, MongoDB and ClickHouse database into your own bucket. Every day one copy is restored and read. Restore, and undo a restore. [Backups](/docs/backups)
- **A watcher** on another server. It knows your apps by itself, and alerts when a server goes silent or a domain is down. [Watcher](/docs/watcher)
- **Updates you run**: no update ever runs by itself. Each release is signed. [Security](/docs/security#updates)
- **The team**: the SSH keys on the server. [The team](/docs/security#the-team)
- **The catalog**: open source apps like Umami, n8n and Vaultwarden, with backups like any app. [Catalog](/docs/catalog)
- **A terminal UI**: `moin` on a terminal shows every server and app, live, with logs and backups. [CLI](/docs/cli#the-terminal-ui)
- **`--json`** on every reading command, for scripts and AI agents. [CLI](/docs/cli)
- **`moin follow`**: the server watches a branch and deploys each new commit. CI needs no key to your server. [Deploy from CI](/docs/deploy#deploy-from-ci)
- **Encrypted backups**: each copy encrypted on the server before it leaves, for the server's key and for yours. [Encryption](/docs/backups#encryption)
- **Security**: public SSH closes after the first deploy, only Tailscale from then on. Cloudflare Tunnel and a firewall with `moin secure`. [Security](/docs/security)
- **moin cloud**: a watcher we run, and a copy your server can add to but never delete. [moin cloud](/docs/cloud)

## Later

- TLS and domain expiry warnings, on the same reports.
- A public status page.
- Heartbeats for your own cron jobs.
