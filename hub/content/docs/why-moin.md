---
title: "Why moin"
description: "Small stack, a daemon that only dials out, backups whose restores are checked"
group: "Start"
order: 3
---

moin is small. It never lets anyone into your server. It keeps your data safe.

## A small stack

moin is two programs you can read. It is not a pile of tools.

| Part | What it is |
| --- | --- |
| `moin` | One Go binary: `moin` is the terminal UI, `moin <command>` the CLI, `moin daemon` the daemon on your servers. |
| Hub | One more Go binary from the same repo: the web app, the API and the MCP server. |
| Web | HTML from the server, with templ and htmx. No Node, no npm tree. |
| Database | SQLite, in a file next to the hub. |

All of it is open source under AGPL-3.0, in one repo: [github.com/axadrn/moin](https://github.com/axadrn/moin).

## The daemon only dials out

Your server calls moin. moin never calls your server.

So nothing on the server waits for moin. The server needs no open port for it. moin holds no SSH key to it.

SSH tools work the other way. The tool holds a key to a root shell, and port 22 stays open.

```text
SSH tools           moin

the tool            moin
 │ holds a key       ▲
 ▼                   │ the daemon
┌───────────┐        │ calls out
│ server    │      ┌─┴─────────┐
│ port 22   │      │ server    │
│ open      │      │ no port   │
└───────────┘      └───────────┘
```

moin's requests come back down the line the daemon opened. The daemon checks each one against your rules.

Three switches can close almost every other door:

| Switch | What closes |
| --- | --- |
| Cloudflare Tunnel | Ports 80 and 443. Visitors come through Cloudflare. |
| SSH only on Tailscale | Port 22, for everyone outside your tailnet. |
| Firewall | Everything else. |

Only `41641/udp` stays open to everyone. It is Tailscale's port for direct connections. [Security](/docs/guides/security) shows how.

A file on the server, `/etc/moin/rules`, says what moin may do there. Only root on the server can change it, so a break-in at moin cannot. [Rules](/docs/guides/security#rules).

## How others reach your servers

From each project's own docs, read November 7, 2026:

| Tool | How it reaches a server |
| --- | --- |
| [Coolify](https://coolify.io/docs/core/how-coolify-works) | "The Coolify instance connects to the selected server over SSH." Its [firewall page](https://coolify.io/docs/core/infrastructure/servers/firewall) lists port 22 (or your SSH port) open to Coolify. SSH can also run [through a Cloudflare Tunnel](https://coolify.io/docs/integrations/networking/cloudflare/tunnels/server-ssh), so the public port can close. |
| [Dokploy](https://docs.dokploy.com/docs/core/ssh-keys) | Remote servers over SSH, with a key you add in Dokploy, as root. |
| [Kamal](https://kamal-deploy.org/docs/configuration/ssh/) | "Kamal uses SSH to connect and run commands on your hosts", as root on port 22 by default, from the machine that runs `kamal`. A proxy or jump host works. |
| [Portainer Edge Agent](https://docs.portainer.io/advanced/edge-agent) | Dials out too: the agent polls the Portainer server and opens a tunnel to it on port 8000. |

## Backups whose restores are checked

moin copies your database into your own bucket, on a schedule you pick. Every day it restores the newest copy into a throwaway database and reads it. If that fails, you get an [alert](/docs/guides/alerts). A restore can be undone. [How backups work](/docs/concepts/backups).

## Every way in

Click in the web. Type in the terminal. Script it with the CLI or the API. Or let an AI agent do it through MCP. Each one does the same work. [Where things work](/docs/reference/surfaces).

## Keyboard first

One box finds everything: `⌘K` in the web, `space` in the terminal UI. A typo is fine. [Palette and keys](/docs/reference/palette).

## Under the hood

- The `moin` binary is Go's standard library plus a few modules: Bubble Tea and Lip Gloss for the terminal UI, `coder/websocket` for the daemon's line, `x/term`, the AWS S3 SDK for your bucket, Litestream and a pure-Go SQLite for backups.
- Built for Linux the way a release builds it (`CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" ./cmd/moin`, Go 1.26.8), the binary is 25.8 MB for arm64 and 28.3 MB for amd64. Measured on November 7, 2026.
- The daemon's line is one WebSocket connection from your server to the hub.

## Next

- [Get started](/docs/getting-started)
- [Security](/docs/guides/security)
- [Self-hosting](/docs/self-hosting)
