---
title: "Getting Started"
description: "From a fresh server to a live app in five minutes"
order: 2
---

## 1. Install ops on your computer

On a Mac or Linux:

```bash
curl -fsSL https://ops.example.com/cli.sh | sh
```

It puts `ops` in `/usr/local/bin`, or in `~/.local/bin` when that is not writable. If it says to add `~/.local/bin` to your PATH, add this line to `~/.zshrc` or `~/.profile` and open a new terminal:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

The commands on these pages name the control plane you read them on.

## 2. Log in

Create a personal API token on the web under **Settings**, **Tokens**. Set **Access** to **Read and write**: a read-only token cannot add servers or deploy. It belongs to your workspace; teammates you invite later work in the same one. Then:

```bash
ops login --url https://ops.example.com
```

Paste the token when it asks. `ops login --url https://ops.example.com --token <token>` works without the prompt.

## 3. Add a server

Any Linux server with root access works, on amd64 or arm64.

```bash
ops servers add web-1
```

It prints one command. Run it on the server within the next hour. Logged in as root and `sudo` is missing? Leave out `sudo`.

```bash
curl -fsSL https://ops.example.com/install.sh | sudo sh -s -- ins_…
```

It installs Docker when it is missing, which takes a few minutes, installs the agent as the `ops-agent` systemd service and connects it. `ops servers` now shows `web-1 online`. The web UI shows the same under **Servers**, live.

## 4. Add your bucket

A bucket is a folder in an online storage service. Your code and your backups go into your own bucket, never to us. Create one at Cloudflare R2, AWS, Backblaze, Hetzner or Exoscale, then add it:

```surfaces
Web: **Bucket** in the sidebar, then **Save bucket**
CLI: ops backups bucket set \
  --endpoint https://<account>.r2.cloudflarestorage.com \
  --bucket my-ops \
  --access-key <key>
TUI: settings tab, **Bucket**, then `enter`
API: PUT /api/v1/backup-storage
MCP: set_backup_storage
```

The CLI asks for the secret key. [Why a bucket](/docs/guides/bucket).

## 5. Deploy

In the repo of your app:

```bash
ops deploy
```

The first deploy creates the app on your only server. ops packs the files git knows, the agent builds and starts them, checks that they stay up and prints the log as it goes:

```text
Created app blog on web-1
Uploading 42 files to your bucket (18.3 KB)
Deploy v1 of blog started
  detected go, service web on port 8080
  building
  …
  v1 is live
```

## 6. Open it

Your app runs, but it has no address yet. Give it a domain, then point the domain at your server with an A record:

```bash
ops domains add blog.example.com
```

ops shows the server's address and the record to set, and the app gets HTTPS. [Domains](/docs/guides/domains) has the details.

Next: [Deploy](/docs/guides/deploy) explains what ops detects and how to configure it.
