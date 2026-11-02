---
title: "FAQ"
description: "Common questions"
order: 6
---

## Does my server need open ports?

Not for ops: the agent dials out. Visitors reach your apps through the proxy on ports 80 and 443 as soon as you add a [domain](/docs/guides/domains). With a [Cloudflare Tunnel](/docs/guides/security#cloudflare-tunnel), not even those are open.

## Does ops build on its own servers?

No. Builds run on your server. `ops deploy` puts your code into your own bucket and a GitHub deploy goes from GitHub straight to your server. The control plane only relays messages.

## How is ops different from Coolify, Kamal or Heroku?

- **No key to your servers.** Coolify Cloud and Dokploy Cloud keep an SSH key to your servers and connect in over SSH; Kamal does the same from your laptop. ops holds no SSH key: the agent on your server dials out, and your server needs no open port for ops. See [Security](/docs/guides/security).
- **Your code and data stay yours.** Heroku, Render, Railway and Fly.io run your code and keep your volumes on their machines. With ops your apps run on your servers, your code goes through your own bucket and your backups live there too.
- **Backups made for SQLite.** Every change copied about once a second, restore to any second of the last 7 days, and a restore check every day. Coolify, Dokploy, Render, Railway and Fly.io offer scheduled or daily backups and snapshots, without a daily restore check.
- **One price.** Unlimited servers, apps and teammates per workspace. No tiers, no per-server fees. Or [host it yourself](/docs/self-hosting), free.

The landing page has the full table, from each tool's own docs and pricing pages.

## What happens when a deploy fails?

The previous release starts again and the deploy is marked failed with the reason. Its log ends with the last lines of the service that stopped: read it with `ops deploys v4`, for the number of the failed deploy.

## Where is my data?

On your server: bind mounts under `/var/lib/ops/apps/<app>/volumes`, named volumes in Docker. Removing an app keeps both.

## Can ops see my data?

Only what you ask for. A table you open, a query you run or the logs you read pass through to you and are never stored, and the activity keeps no SQL and no values. To close even that, write `data: off` and `logs: off` into `/etc/ops/rules` on your server: the agent then refuses every read, and nobody can change it through ops, not even us. See [Rules](/docs/guides/security#rules).

## Where are my env vars?

Today in the control plane, sealed with AES-256-GCM, and handed to your server with each deploy. Next they move to your servers only, so hosted ops keeps your login and nothing else. After that comes end-to-end encryption: we can't read them at all.

## What is a workspace?

Your team in ops: its servers, apps, bucket, settings and people. Invite teammates from the **Team** page and everyone works on the same servers. The plan, the trial and the [activity](/docs/guides/activity) belong to the workspace. You can be in several workspaces and switch between them at the top of the sidebar.

## Do I pay per workspace?

Yes. One price per workspace, with unlimited servers, apps and teammates. No tiers, no per-server fees. Most teams need one. A second workspace, for example for a client, has its own plan, and the free trial comes once, with your first workspace.

## How does the hosted trial work?

Your first workspace gets 30 days with everything, without a card. Afterwards the workspace is read-only until you subscribe: your apps keep running, their backups too, and you can still see everything, stop and start apps and manage your account and team.

## Will the name change?

Maybe. If it does, your servers, the CLI and the agents move over by themselves, and the old address keeps working for a while. Nothing breaks.

## Can I see and change everything from an AI agent?

Almost everything. Every action on servers, apps, data, backups, domains and invitations is an [MCP tool](/docs/guides/mcp). Your account, roles and deleting a workspace stay on the web, so a leaked token can never take them over.

## Is ops open source?

Yes, all of it, under AGPL-3.0 at [github.com/axadrn/ops](https://github.com/axadrn/ops): the `ops` binary (CLI, terminal UI, agent) and the control plane. We host the control plane for you, and you may host it yourself.

## Why AGPL, and can I contribute?

AGPL keeps ops open: whoever runs a changed ops as a service has to share the changes. Pull requests are welcome. On your first one a bot asks you to agree once to the [Contributor License Agreement](https://github.com/axadrn/ops/blob/main/CLA.md): your contribution comes in under Apache 2.0, which keeps the license of ops flexible, and you keep your copyright. See [CONTRIBUTING.md](https://github.com/axadrn/ops/blob/main/CONTRIBUTING.md).

## Can my team work on it too?

Yes, and it costs nothing extra. [Invite them](/docs/guides/team#invite-someone) from the **Team** page or with `ops team invite`; everyone works on the same servers and apps, and the [activity](/docs/guides/activity) shows who deployed or changed what.
