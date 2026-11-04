---
title: "FAQ"
description: "Common questions"
order: 6
---

## Does my server need open ports?

Not for moin: the agent dials out. Visitors reach your apps through the proxy on ports 80 and 443 as soon as you add a [domain](/docs/guides/domains). With a [Cloudflare Tunnel](/docs/guides/security#cloudflare-tunnel), not even those are open.

## Does moin build on its own servers?

No. Builds run on your server. `moin deploy` puts your code into your own bucket and a GitHub deploy goes from GitHub straight to your server. The control plane only relays messages.

## How is moin different from Coolify, Kamal or Heroku?

- **No key to your servers.** Coolify Cloud and Dokploy Cloud keep an SSH key to your servers and connect in over SSH; Kamal does the same from your laptop. moin holds no SSH key: the agent on your server dials out, and your server needs no open port for moin. See [Security](/docs/guides/security).
- **Your code and data stay yours.** Heroku, Render, Railway and Fly.io run your code and keep your volumes on their machines. With moin your apps run on your servers, your code goes through your own bucket and your backups live there too.
- **Backups made for SQLite and Postgres.** Every SQLite change copied about once a second, every Postgres change within a minute, restore to any second of the last 7 days, and a restore check every day, all in your own bucket. Coolify and Dokploy back up on a schedule; Heroku, Render, Railway and Fly.io restore Postgres to a point in time on their side, and none of them checks a restore every day.
- **One price.** Unlimited servers, apps and teammates per workspace. No tiers, no per-server fees. Or [host it yourself](/docs/self-hosting), free.

The landing page has the full table, from each tool's own docs and pricing pages.

## Do I need a domain?

Not to start. After its first deploy every app answers at its [instant address](/docs/guides/domains#instant-address), such as `https://shop.203-0-113-7.sslip.io`, with HTTPS and a password in front until you open it. It needs ports 80 and 443, so a server behind a Cloudflare Tunnel gets none. Add your own domain when you are ready, one per service if you like.

## Can I install apps like n8n or Umami?

Yes: 47 open-source apps from the [catalog](/docs/guides/catalog), in one click. moin makes their secrets, backs up their database and tells you when a new release is out; an update takes a snapshot first.

## What happens when a deploy fails?

The previous release starts again and the deploy is marked failed with the reason. Its log ends with the last lines of the service that stopped: read it with `moin deploys v4`, for the number of the failed deploy.

## Where is my data?

On your server: bind mounts under `/var/lib/moin/apps/<app>/volumes`, named volumes in Docker. Removing an app keeps both.

## Can moin see my data?

Only what you ask for. A table you open, a query you run or the logs you read pass through to you and are never stored, and the activity keeps no SQL and no values. To close even that, write `data: off` and `logs: off` into `/etc/moin/rules` on your server: the agent then refuses every read, and nobody can change it through moin, not even us. See [Rules](/docs/guides/security#rules).

## Where are my env vars?

Today in the control plane, sealed with AES-256-GCM, and handed to your server with each deploy. Next they move to your servers only, so hosted moin keeps your login and nothing else. After that comes end-to-end encryption: we can't read them at all.

## What is a workspace?

Your team in moin: its servers, apps, bucket, settings and people. Invite teammates from the **Team** page and everyone works on the same servers. The plan, the trial and the [activity](/docs/guides/activity) belong to the workspace. You can be in several workspaces and switch between them at the top of the sidebar.

## Do I pay per workspace?

Yes. One price per workspace, with unlimited servers, apps and teammates. No tiers, no per-server fees. Most teams need one. A second workspace, for example for a client, has its own plan, and the free trial comes once, with your first workspace.

## How does the hosted trial work?

Your first workspace gets 30 days with everything, without a card. Afterwards the workspace is read-only until you subscribe: your apps keep running, their backups too, and you can still see everything, stop and start apps and manage your account and team.

## Why moin?

**moin** /mɔɪn/ · Hamburg for hello. Any hour, one word.

I grew up in Hamburg, next to one of Europe's busiest container ports. Millions of boxes a year, packed once, shipped anywhere, and nothing goes missing. Your apps are containers too. moin ships them to your servers and keeps every change to their data. Deploy anything. Lose nothing.

## Can I see and change everything from an AI agent?

Almost everything. Every action on servers, apps, data, backups, domains and invitations is an [MCP tool](/docs/guides/mcp). Your account, roles and deleting a workspace stay on the web, so a leaked token can never take them over.

## Is moin open source?

Yes, all of it, under AGPL-3.0 at [github.com/axadrn/moin](https://github.com/axadrn/moin): the `moin` binary (CLI, terminal UI, agent) and the control plane. We host the control plane for you, and you may host it yourself.

## Why AGPL, and can I contribute?

AGPL keeps moin open: whoever runs a changed moin as a service has to share the changes. Open an issue first, then a pull request. On your first one a bot asks you to agree once to the [Contributor License Agreement](https://github.com/axadrn/moin/blob/main/CLA.md): your contribution comes in under Apache 2.0, which keeps the license of moin flexible, and you keep your copyright. See [CONTRIBUTING.md](https://github.com/axadrn/moin/blob/main/CONTRIBUTING.md).

## Can my team work on it too?

Yes, and it costs nothing extra. [Invite them](/docs/guides/team#invite-someone) from the **Team** page or with `moin team invite`; everyone works on the same servers and apps, and the [activity](/docs/guides/activity) shows who deployed or changed what.
