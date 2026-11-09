---
title: "FAQ"
description: "Common questions, short answers"
group: "Reference"
order: 6
---

The questions people ask most.

## Does my server need open ports?

Not for moin. The daemon dials out. Visitors reach your apps on ports 80 and 443, and with a [Cloudflare Tunnel](/docs/guides/security#cloudflare-tunnel) not even those are open.

## Does moin build on its own servers?

No. Builds run on your server. [How moin works](/docs/concepts/how-moin-works).

## How is moin different from Coolify, Kamal or Heroku?

moin holds no SSH key to your servers. Your code and backups stay in your own bucket. Every day moin checks that a copy restores. [Why moin](/docs/why-moin).

## Do I need a domain?

Not to start. Every app answers at its [instant address](/docs/guides/domains#instant-address), with HTTPS and a password.

## Can I install apps like n8n or Umami?

Yes: 47 open source apps from the [catalog](/docs/guides/catalog), in one click.

## What happens when a deploy fails?

The previous deploy starts again. The failed one says why. `moin deploys v4` shows its log. The same happens when the server restarts during a deploy, as soon as it is back.

## Where is my data?

On your server: bind mounts under `/var/lib/moin/apps/<app>/volumes`, named volumes in Docker. Removing an app keeps both.

## Does moin back up my files?

No. moin backs up databases, not files. [Backups](/docs/guides/backups#what-moin-backs-up).

## Can moin see my data?

Only what you open, passed through to you and never stored. `data: off` and `logs: off` in `/etc/moin/rules` close even that. [Rules](/docs/guides/security#rules).

## Where are my env vars?

In the hub, sealed with AES-256-GCM, and handed to your server with each deploy. [Roadmap](/docs/roadmap) says what comes next.

## What is a workspace?

Your team in moin: its servers, apps, bucket, settings and people. You can be in several. [Team](/docs/guides/team).

## Do I pay per workspace?

Yes: $29 a month per workspace ($249 a year). Unlimited servers, apps and team. The free trial comes once, with your first. Hosting moin yourself is free.

The tenth server costs the same as the first. On hosted plans, Coolify Cloud is $5 for 2 servers plus $3 per extra server, so it reaches $29 at 10 servers and $59 at 20. Dokploy Cloud Startup is from $15 with 3 servers plus $4.50 per extra server, so about $29 at 6 servers.

The whole team is included, with no per-seat fee, and so is everything else: backups with a daily check, uptime, the data browser and AI agents. No higher tier unlocks more.

## How does the hosted trial work?

Your first workspace gets 30 days with everything, no card. Then it is read-only until you subscribe. Your apps and their backups keep running.

## Can an AI agent do everything?

Almost. Every action on servers, apps, data, backups, domains and invitations is an [MCP tool](/docs/guides/mcp). Your account, roles and deleting a workspace stay on the web.

## Can my team work on it too?

Yes, at no extra cost. [Invite them](/docs/guides/team#invite-someone).

## Is moin open source?

Yes, all of it, under AGPL-3.0 at [github.com/axadrn/moin](https://github.com/axadrn/moin). [Host it yourself](/docs/self-hosting), free.

## Why AGPL, and can I contribute?

AGPL keeps moin open: whoever runs a changed moin as a service shares the changes. Open an issue first, then a pull request. On your first one you agree once to the [Contributor License Agreement](https://github.com/axadrn/moin/blob/main/CLA.md). [CONTRIBUTING.md](https://github.com/axadrn/moin/blob/main/CONTRIBUTING.md).

## Why the name?

**moin** /mɔɪn/ is Hamburg for hello, at any hour. Hamburg is a container port. Your apps are containers too, and moin ships them to your servers.
