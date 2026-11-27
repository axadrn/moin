---
title: "Get started"
description: "From a fresh server to a live app in about two minutes. One command: moin deploy prod."
group: "Start"
order: 1
---

moin puts your app on your own server. It backs up its database, and every day it checks that the backup comes back.

You need two things:

- A Linux server you can reach with `ssh prod`, as root. Any provider.
- Your app in a git repository on your laptop.

That is all. No account. No Docker on your laptop.

## 1. Install moin

On your Mac or Linux laptop:

```bash
curl -fsSL https://moin.example.com/cli.sh | sh
```

Later, `moin update` updates it.

## 2. Deploy

Go to your app's folder and deploy to your server:

```bash
cd shop
moin deploy prod
```

`prod` is the name you use with `ssh`. If `ssh prod` works, `moin deploy prod` works.

```text
shop · 3f2a9c1 → prod
Set up moin on prod and deploy shop? [y/N] y
✓ moin. shop is live: https://shop.203-0-113-7.sslip.io
  Postgres found. No bucket yet: moin bucket set prod
```

Your app is live, with HTTPS. On a fresh server this takes about two minutes. Most of it is installing Docker.

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
Close public SSH, only Tailscale from now on? [Y/n]
```

Press Enter. From now on, nobody on the internet can reach port 22. You still reach the server, over Tailscale. [Security](/docs/security#ssh-only-over-tailscale)

## 4. Back it up

moin finds SQLite and Postgres by itself. It needs a place to put the copies: a bucket, at any S3-compatible provider.

```bash
moin bucket set prod
```

moin asks for the endpoint, the bucket's name, the access key and the secret key. The secret never lands in your shell history. From then on it copies every database on a schedule, and checks one copy every day. [Backups](/docs/backups)

## 5. Add your domain

```bash
moin domains add shop shop.example.com
```

Where you bought the domain, add an A record: `shop.example.com` to your server's IP. HTTPS comes by itself once the DNS points at the server. [Deploy](/docs/deploy#domains)

## If something breaks

- **`ssh prod` does not work.** Fix that first. moin goes the same way.
- **The build fails.** moin shows why. The live deploy keeps running.
- **The new deploy does not start.** moin says why, and the deploy before it runs again.
- **The first deploy fails halfway.** Run `moin deploy prod` again. What is installed already stays.

Next: [how it works](/docs/how-it-works), in one picture.
