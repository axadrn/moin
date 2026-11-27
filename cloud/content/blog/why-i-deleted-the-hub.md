---
title: "Why I deleted the hub"
description: "moin had a hub that every server trusted. I deleted it. Now commands go one way over your own SSH, reports go the other, and nothing in between holds a key."
date: "2026-11-27"
---

moin had a hub. I deleted it. This is why, and what you get instead.

## What the hub was

The hub was the middle of moin. A web app, an API, an MCP server, accounts, passkeys, teams. On every server a daemon kept a line open to it and did what it said: deploy this, restart that, restore this backup. You ran the hub yourself, or used the hosted one.

It worked. And it got wide. Every feature lived in five places. Before your first app was live, you installed a CLI, signed in, added a server, copied an install command and connected a GitHub App.

## One place every server trusts

The hub could command every server behind it. So whoever took over the hub could too.

I built a lot to make that smaller. Signed updates, so a hacked hub could not hand a daemon its own binary. A rules file on each server, so it could not start a privileged container. Secrets the hub could set but never read.

Each made the damage smaller. None made it zero. A hub that can deploy can deploy something bad. That is what a hub is for.

A hosted hub is worse. One break-in there reaches every server of every customer. I did not want to run that place. Nobody should have to trust me that much.

## Who I build for

```bash
ssh prod
git pull && ./deploy.sh
```

This person is right. It is simple, and it works. Their SSH key is the only way in.

What they lack is small. A backup that really comes back. A way back after a bad deploy. Someone who notices when the server is gone. So that is what moin is now: their workflow, plus those three things.

## Two roads, each one way

```text
your laptop
   │ ssh, your key: commands
   ▼
┌──────────────────────────┐   reports only, out   ┌──────────────┐
│ server                   │──────────────────────▶│ watcher      │
│  moin (run over ssh)     │                       │ yours, or    │
│  moin daemon (timers)    │── backups ──▶ bucket  │ moin cloud   │
└──────────────────────────┘                       └──────────────┘
```

**Commands go in.** From your laptop to your server, over SSH, with your key. `moin deploy prod` is `ssh prod`, then the work on the server.

**Reports go out.** Every minute the daemon tells a watcher what it sees: the apps, the domains, the backups, the last check. The watcher answers with a status code. The daemon acts on nothing in it.

Who can command your server? Whoever can SSH to it as root. Not a watcher. Not moin cloud. Not me. The daemon only runs timers. It takes no commands and opens no port.

## What you get now

**One command.** In your app's folder:

```bash
moin deploy prod
```

If `ssh prod` works, this works. The first time, it installs Docker and moin on the server. It sends your committed code over SSH, and the server builds it. It ends in a link with HTTPS, before you touched DNS.

**Closed SSH.** Right after the first deploy, moin asks to close port 22. It puts the server in your tailnet, opens a new session over Tailscale, and closes port 22 only when that works. `moin secure prod` puts the web behind a Cloudflare Tunnel and a firewall around the rest. Then no public TCP port is left. Locked out? `moin unlock` in your provider's web console opens port 22 again.

**Proven backups.** moin finds SQLite and Postgres by itself. Every copy is encrypted on the server before it goes to your bucket. Every day, moin restores the newest copy and reads it. A backup you never restored is a hope.

**Knocks.** Bots try every new server within minutes. Once port 22 is closed, the firewall drops them and counts them. `moin knocks prod` shows how many, by port. They knock. Nothing opens.

## What moin cloud is, and isn't

A dead server can't tell you it's dead. Someone else has to notice. That is a watcher. Run your own with `moin watcher run`, on a second small server. Or let moin cloud run one.

moin cloud receives the reports. When they stop, or a domain is down from outside, it mails you. It can't deploy, restart or stop anything. It has no key to your server. If it goes down, your apps and backups keep running. It sees names, domains, versions and times. Never your data, never your env.

It costs $7 a month or $49 a year, for all your servers. The first 30 days are free.

## What costs nothing

moin itself. All of it is open source: the CLI, the daemon, the watcher, moin cloud too. No account. Deploys, rollbacks, backups to your own bucket, the daily check, alerts, your own watcher. Free. The free software sends nothing home.

## What I gave up

- No web dashboard. The CLI is the interface, with `--json` for scripts and AI agents.
- No roles. Whoever has SSH to the server as root is on the team. That was true before too: a tool that runs your code can do anything on that server.
- `moin follow` deploys each new commit of a branch. Then whoever can push to that branch can deploy.
- `moin secure` needs Tailscale, and Cloudflare for your domain. Cloudflare sees the web traffic it carries.

I deleted most of the code to get here. What is left is easier to read, and easier to trust.

[Get started](/docs/getting-started). It takes about two minutes.
