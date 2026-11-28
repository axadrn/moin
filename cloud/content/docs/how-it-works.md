---
title: "How it works"
description: "Two one-way roads. Commands go from you to your server over SSH. Reports go from your server to a watcher. Nothing comes back."
group: "Start"
order: 2
---

moin has two roads, and each goes one way.

- **Commands go in.** From your laptop to your server, over SSH, with your key.
- **Reports go out.** From your server to a watcher, which tells you when something is wrong.

Nothing travels the other way. A watcher can't send your server a command. Nobody can, except someone with SSH to it.

One answer counts: after a copy, moin cloud answers with one address to upload that copy to. The daemon uses it only over https, only to a public address, for one PUT, and follows no redirect. [moin cloud](/docs/cloud#a-copy-your-server-cant-delete)

```text
your laptop
   │ ssh, your key: commands
   ▼
┌──────────────────────────┐   reports only, out   ┌──────────────┐
│ server                   │──────────────────────▶│ watcher      │
│  moin (run over ssh)     │                       │ yours, or    │
│  moin daemon (timers)    │── backups ──▶ bucket  │ moin cloud   │
│                          │── a copy ──▶ moin cloud, when paid   │
│                          │── alerts while alive ─▶ Telegram, …  │
└──────────────────────────┘                       └──────────────┘
```

## The pieces

There is one program, `moin`. The same file runs in three places.

| Where | What it does |
| --- | --- |
| your laptop | `moin deploy my-server` and every other command. It logs in to the server over SSH and runs `moin` there. |
| the server | `moin daemon` runs the timers: backups, the daily restore check, a report to the watchers every minute, alerts, the branches and images apps follow, the count of knocks, and a daily cleanup of old images. It takes no commands. Its systemd service is `moin-daemon`. |
| another server | `moin watcher run`, if you run your own. [Watcher](/docs/watcher) |

## Commands go in

`moin deploy my-server` is `ssh my-server`, then the work on the server. There is no service in between.

- Who can command your server: whoever can SSH to it as root. Nobody else.
- moin opens no port for itself. After the first deploy, public SSH closes too, and SSH comes only over Tailscale. [Security](/docs/security)
- moin runs as root on the server. For a user who is not root, it runs `sudo` by itself.
- Your laptop keeps a list of the servers you deployed to, in `~/.config/moin/servers`. `moin` alone shows them. On a new laptop, `moin servers add my-server` puts one back.

## Reports go out

Every minute the daemon tells each watcher what it sees: the apps, their domains, the backups, the last restore check and the knocks of the day. The watcher answers with a status code, and the daemon acts on nothing in it.

- The daemon alerts what it sees while it lives: a backup that failed, a check that failed, an app that crashes, a full disk.
- A watcher alerts what a dead server can't: that it went silent, or that a domain is down from outside.

## The way back

There is one way back, and you take it yourself: a restore. Your CLI asks the server to fetch a copy from the bucket and put it back. From moin cloud, your CLI gets a download address and hands it to the server over SSH, with your backup key. [Backups](/docs/backups#restore)

## What a deploy does

1. Your laptop sends the committed source of the folder over SSH, as a tar file.
2. The server builds it. With a `Dockerfile` or a `compose.yml`, from those. Without, moin builds it with [Railpack](https://railpack.com).
3. It starts the new deploy and waits until it runs.
4. It points your domains at it, with HTTPS from Let's Encrypt.
5. If the new deploy does not come up, the one before runs again.

No registry, no build on your laptop, no CI needed. Want a registry anyway? `moin deploy my-server --image ghcr.io/you/my-app:1.4` makes the server pull the image, and `moin registry set` logs it in to a private one. [Deploy](/docs/deploy#deploy-an-image-from-your-registry)

## Where things are on the server

| Path | What |
| --- | --- |
| `/usr/local/bin/moin` | the program |
| `/etc/moin/config.yml` | the server's name, the bucket, your backup key's public half, the alerts, the watchers and the backup schedule. Root only. [Server config](/docs/config) |
| `/etc/moin/backup.key` | the server's own backup key. Root only. [Encryption](/docs/backups#encryption) |
| `/var/lib/moin/server.json` | what is closed: public SSH, the firewall, the tunnel. Root only. [Security](/docs/security) |
| `/var/lib/moin/apps/<app>/` | each app: its releases, its env, its data |
| `/var/lib/moin/apps/<app>/app.env` | the app's env vars. Root only. |
| `/var/lib/moin/apps/<app>/volumes/` | the data of folders like `./data` in your compose file |

Each app runs as plain Docker Compose. Nothing in it needs moin to run.
