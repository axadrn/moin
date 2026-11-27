---
title: "Get started"
description: "From a fresh server to a live app in about two minutes. One command: moin deploy prod."
group: "Start"
order: 1
---

moin puts your app on your own server. It backs up its database, and every day it checks that the backup comes back.

You need two things:

- A Linux server you can reach with `ssh prod`, as root or with `sudo`. 64-bit, Intel or ARM. Any provider.
- Your app in a git repository on your laptop.

That is all. No account. No Docker on your laptop.

Never rented a server? Start with the [Basics](/docs/basics/servers), or follow [from zero to live](/docs/basics/from-zero-to-live).

## 1. Install moin

On your Mac or Linux laptop:

```bash
curl -fsSL https://moin.run/cli.sh | sh
```

It downloads `moin` from the newest GitHub release, and installs it only when the release's signature checks out. It puts it in `/usr/local/bin`, or in `~/.local/bin` when that is not writable. Later, `moin update` updates it.

The check needs `ssh-keygen` from OpenSSH 8.1 or newer. Every Mac has it, and so does almost every Linux. [Updates](/docs/security#updates)

## 2. Deploy

Go to your app's folder and deploy to your server:

```bash
cd shop
moin deploy prod
```

`prod` is the name you use with `ssh`. If `ssh prod` works, `moin deploy prod` works.

```text
shop · 3f2a9c1 → prod
? Set up moin on prod and deploy shop? [y/N] y
→ installing moin v1.2.3
→ starting the daemon: backups, restore checks, reports
→ building
→ starting
→ v1 is live
✓ moin. shop is live: https://shop.203-0-113-7.sslip.io
  Postgres found. No backup yet: moin bucket set prod
```

Your app is live, with HTTPS. On a fresh server this takes about two minutes. Most of it is installing Docker. What Docker and the build print shows dimmed, between the steps.

## What happened

1. moin logged in to `prod` over SSH, with your key.
2. It installed Docker and itself on the server. Only the first time.
3. It sent the committed source of the folder. Changes you did not commit stay on your laptop.
4. The server built the app and started it.
5. It gave the app an address that needs no DNS: `https://shop.203-0-113-7.sslip.io`.

The app is named after the folder: `shop`. From now on, `moin deploy` in the folder is enough.

## 3. Close public SSH

Right after the first deploy, moin asks:

```text
? Close public SSH, only Tailscale from now on? [Y/n]
```

Press Enter. moin puts the server in your tailnet, your own private network. It makes sure a new SSH session over Tailscale works, and only then closes port 22 to the internet. If you can't get back in within 2 minutes, port 22 opens again by itself.

Then point `prod` at the tailnet, as moin prints it, in `~/.ssh/config`. From then on, nobody on the internet can reach port 22. It needs Tailscale on your laptop, and a Tailscale account. The free plan works. [Security](/docs/security#ssh-only-over-tailscale)

## 4. See it

```bash
moin
```

On a terminal, this opens the terminal UI: every server and every app, live. Pick an app and press Enter for its logs and backups. `q` quits. [The terminal UI](/docs/cli#the-terminal-ui)

## 5. Back it up

moin finds SQLite and Postgres by itself. It needs a place to put the copies: a bucket, at any S3-compatible provider.

```bash
moin bucket set prod
```

moin asks for the endpoint, the bucket's name, the access key and the secret key. The secret never lands in your shell history. The server tries the bucket first, and keeps it only when that works.

```text
✓ prod backs up to backups. The first copies come within a minute
```

Then moin shows your backup key, once. Every copy is encrypted for it. Put it in your password manager, and paste it back: moin opens a fresh copy with it, to prove it works. When the server is gone, only this key opens its copies.

From then on it copies every database every hour, and every day it restores the newest copy and reads it. [Backups](/docs/backups)

## 6. Watch it

A dead server can't tell you it's dead. A watcher on another machine notices. Set one up in four commands, or let us run one: `moin cloud login`, then `moin cloud connect prod`. [Watcher](/docs/watcher#set-one-up)

## 7. Add your domain

```bash
moin domains add shop shop.example.com
```

Where you bought the domain, add an A record: `shop.example.com` to your server's IP. HTTPS comes by itself once the DNS points at the server. [Deploy](/docs/deploy#domains)

## If something breaks

- **`ssh prod` does not work.** Fix that first. moin goes the same way.
- **You log in as a user, not as root.** moin runs `sudo` for you. It needs sudo without a password.
- **The build fails.** moin shows why. The live release keeps running.
- **The new deploy does not start.** moin says why, and the deploy before it runs again.
- **The first deploy fails halfway.** Run `moin deploy prod` again. What is installed already stays.
- **`cli.sh` says the signature does not check out.** `ssh -V` shows your OpenSSH. Older than 8.1? Update it, then run `cli.sh` again.
- **Locked out after closing SSH.** In your provider's web console, run `/usr/local/bin/moin unlock`. [The way back in](/docs/security#the-way-back-in)
