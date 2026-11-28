---
title: "From zero to live"
description: "One tutorial, start to end. Rent a server, log in with a key, deploy your app, add a domain, close every port, back it up and restore it."
group: "Basics"
order: 10
---

No server yet. No idea where to start. This page takes you from nothing to a live app with checked backups, step by step. Every step links to the page that explains it.

```text
rent → key → install → deploy → close SSH → domain → secure → bucket → restore → watch
```

## 1. Rent a server

At a provider like Hetzner or DigitalOcean, make a server: Ubuntu LTS, 2 vCPU, 4 GB, with an IPv4 address. Paste your public key in the form, from step 2. Note its IP, here `203.0.113.7`. [What is a server](/docs/basics/servers)

## 2. A key and a name

On your laptop, if you have no key yet:

```bash
ssh-keygen -t ed25519 -C "ada@laptop"
cat ~/.ssh/id_ed25519.pub
```

Then name the server in `~/.ssh/config`:

```text
Host prod
  HostName 203.0.113.7
  User root
```

Check it: `ssh prod`, type `yes` once, then `exit`. [SSH and keys](/docs/basics/ssh)

## 3. Install moin

On your laptop:

```bash
curl -fsSL https://moin.run/cli.sh | sh
```

It installs `moin` only when the release's signature checks out. [Get started](/docs/getting-started)

## 4. Deploy

Your app must be in git, with a commit. moin sends what is committed.

```bash
cd shop
moin deploy prod
```

```text
shop · 3f2a9c1 → prod
? Set up moin on prod and deploy shop? [y/N] y
→ installing Docker
→ installing moin v1.2.3
→ starting the daemon: backups, restore checks, reports
→ building
→ starting
→ v1 is live
✓ moin. shop is live: https://shop.203-0-113-7.sslip.io
  Postgres found. No backup yet: moin bucket set prod
```

The first time it installs Docker and moin on the server. That takes about two minutes. The app is named after the folder.

moin looks at your folder, and builds it like this:

| Your folder has | moin builds |
| --- | --- |
| `compose.yml` | the compose app, as it is |
| `Dockerfile` | that Dockerfile |
| `go.mod` | your Go app, with a Dockerfile moin writes |
| `package.json` | Node, or Bun with a Bun lock file, with [Railpack](https://railpack.com) |
| `requirements.txt` or `pyproject.toml` | Python, with Railpack |
| `Gemfile` | Ruby and Rails, with Railpack |
| `composer.json` or `index.php` | PHP, with Railpack |

Railpack says what it found, like `→ Railpack: Node 24.11.1, start: npm run start`.

Without a compose file, your app listens on `PORT`, which is `8080`, on every address, not only `localhost`. A SQLite file goes in `DATA_DIR`, which is `/data`, so it is kept and backed up. Settings and secrets go in its env: `moin env edit shop`. [Docker and compose](/docs/basics/docker)

## 5. Close public SSH

Right after the first deploy, moin asks:

```text
? Close public SSH, only Tailscale from now on? [Y/n]
```

Install Tailscale on your laptop first, and log in. Then press Enter. moin puts the server in your tailnet, tries SSH over it, and only then closes port 22. It prints the new `HostName` for `~/.ssh/config`. Change it. [Tailscale](/docs/basics/tailscale)

## 6. A domain

```bash
moin domains add shop shop.example.com
```

Where your domain lives, add an A record: `shop` → `203.0.113.7`. HTTPS follows by itself. [Domains and DNS](/docs/basics/dns)

## 7. Close the rest

```bash
moin secure prod
```

It asks for a Cloudflare Tunnel token. Paste it, and send `shop.example.com` to `http://moin-traefik:8080` in the tunnel's Public Hostnames. Now no public TCP port is left. Enter without a token keeps the web on 80 and 443, and closes everything else. [Cloudflare Tunnel](/docs/basics/cloudflare-tunnel)

## 8. A bucket and your key

Make a bucket at any S3-compatible provider, best not the one of your server. Then:

```bash
moin bucket set prod
```

It asks for the endpoint, the bucket name, the access key and the secret key. Then it shows your backup key, once:

```text
Your backup key for prod. It is shown once.
  AGE-SECRET-KEY-1…
? Paste your backup key, to confirm that you have it:
✓ Your key opens the copies of prod: it read the copy 20261127T140000Z-118204 of postgres-db from the bucket to the end
```

Save it in your password manager first. Then paste it back. [What a backup is](/docs/basics/backups)

## 9. See a backup, and restore it

```bash
moin backups shop
moin restore shop
```

`moin backups` lists the copies, and the last daily restore check. `moin restore` lets you pick a copy, and keeps the data from before as an undo. Try it once now, on a quiet day. [Backups](/docs/backups#restore)

## 10. Someone who notices

A dead server can't tell you it's dead. Let moin cloud watch it:

```bash
moin watcher connect prod
```

The first time, the browser signs your laptop in.

```text
✓ prod reports to moin cloud every minute
```

Or run your own watcher, on another machine. [Watcher](/docs/watcher#set-one-up)

## Done

Your app is live on your domain. No port is open to the internet. Every hour a copy goes to your bucket, and every day one is restored and read. If the server stops, you hear about it.

From now on, a new release is one line, in the app's folder:

```bash
moin deploy
```
