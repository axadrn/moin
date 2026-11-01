---
title: "FAQ"
description: "Common questions"
order: 6
---

## Does my server need open ports?

Not for ops: the agent dials out. Visitors reach your apps through the proxy on ports 80 and 443 as soon as you add a [domain](/docs/guides/domains). A Cloudflare Tunnel with no open port at all is coming next.

## Does ops build on its own servers?

No. Builds run on your server. The control plane stores the source and relays messages.

## What happens when a deploy fails?

The previous release starts again and the deploy is marked failed with the reason. Read it with `ops logs`.

## Where is my data?

On your server: bind mounts under `/var/lib/ops/apps/<app>/volumes`, named volumes in Docker. Removing an app keeps both.

## Can ops see my data?

Only what you ask for. A table you open, a query you run or the logs you read pass through to you and are never stored, and the activity keeps no SQL and no values. To close even that, write `data: off` and `logs: off` into `/etc/ops/rules` on your server: the agent then refuses every read, and nobody can change it through ops, not even us. See [Rules](/docs/guides/security#rules).

## Where are my env vars?

Today in the control plane, sealed with AES-256-GCM, and handed to your server with each deploy. Next they move to your servers only, so hosted ops keeps your login and nothing else. After that comes end-to-end encryption: we can't read them at all.

## How does the hosted trial work?

A new account gets 30 days with everything, without a card. Afterwards the workspace is read-only until you subscribe: your apps keep running, their backups too, and you can still see everything, stop and start apps and manage your account and team.

## Will the name change?

Maybe. If it does, your servers, the CLI and the agents move over by themselves, and the old address keeps working for a while. Nothing breaks.

## Can I see and change everything from an AI agent?

Yes. Every action is an [MCP tool](/docs/guides/mcp).

## Is ops open source?

Yes, all of it, under AGPL-3.0 at [github.com/axadrn/ops](https://github.com/axadrn/ops): the `ops` binary (CLI, terminal UI, agent) and the control plane. We host the control plane for you, and you may host it yourself.

## Can my team work on it too?

Yes, and it costs nothing extra. Invite them from the **Team** page or with `ops team invite`; everyone works on the same servers and apps, and the [activity](/docs/guides/team) shows who deployed or changed what.
