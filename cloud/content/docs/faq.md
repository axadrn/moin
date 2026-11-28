---
title: "FAQ"
description: "Short answers. What moin is. What it needs. Where your data is. Who can reach your server. The license."
group: "Reference"
order: 3
---

## What is moin?

Deploy anything. Lose nothing. Like `ssh my-server` and `./deploy.sh`, in one command. One command puts your app on your server. Every backup is restored and checked, every day. Nothing but your SSH key can command your server.

## Is there a dashboard?

No. The CLI is the interface, with `--json` for scripts and AI agents. `moin` alone opens a terminal UI. A watcher has one small page that only shows. [CLI](/docs/cli)

## Do I need an account?

No. moin cloud is the one part with an account. [moin cloud](/docs/cloud)

## Do I need Docker?

Not on your laptop. moin installs Docker on the server. Only an image you built on your laptop needs Docker there: `moin deploy --image my-app:dev` sends it.

## Which servers work?

Any 64-bit Linux server, Intel or ARM, you can reach with SSH. As root, or as a user with sudo without a password. Any provider.

## Does moin build on its own servers?

No. Your server builds your app. Your source goes from your laptop to it over SSH, and nowhere else.

## Do I need a domain?

Not to start. Every app answers at an address that needs no DNS, like `https://my-app.203-0-113-7.sslip.io`, with HTTPS.

## Where is my data?

On your server, under `/var/lib/moin/apps/<app>/volumes`, and in Docker's named volumes. The copies are in your bucket.

## Does moin back up my files?

No. moin backs up databases: SQLite, Postgres, MySQL and MariaDB. [Backups](/docs/backups)

## Can my team deploy?

Everyone whose SSH key may log in as root can. `moin keys add my-server anna.pub`. [The team](/docs/security#the-team)

## Can an AI agent use it?

Yes, through the CLI, like you do. [AI agents](/docs/cli#ai-agents)

## How is moin different from Coolify or Kamal or uncloud?

Coolify and Dokploy are dashboards that hold a key to your server. Kamal and uncloud run over SSH like moin, and do several servers. moin restores and checks your backups every day, and a watcher notices when a server is gone. [Every fact, with its source](/compare)

## Does my server update by itself?

Never. `moin update my-server` updates it, when you run it. [Updates](/docs/security#updates)

## Is moin open source?

Yes, all of it: Apache-2.0, at [github.com/axadrn/moin](https://github.com/axadrn/moin). Free.

## Can I contribute?

Yes. Open an issue first, then a pull request. Your contribution comes in under the same license, Apache-2.0. [CONTRIBUTING.md](https://github.com/axadrn/moin/blob/main/CONTRIBUTING.md).

## Why the name?

**moin** /mɔɪn/ is Hamburg for hello, at any hour. Hamburg is a container port. Your apps are containers too, and moin ships them to your servers.
