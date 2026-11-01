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

It puts `ops` in `/usr/local/bin`, or in `~/.local/bin` when that is not writable. The commands on these pages name the control plane you read them on.

## 2. Log in

Create a personal API token on the web under **Settings**, **Tokens**, with read and write access. It belongs to your workspace; teammates you invite later work in the same one. Then:

```bash
ops login
```

Paste the token when it asks. `ops login --url <control plane> --token <token>` works without the prompt.

## 3. Add a server

Any Linux server with root access works, on amd64 or arm64.

```bash
ops servers add web-1
```

It prints one command. Run it on the server as root within the next hour:

```bash
curl -fsSL https://ops.example.com/install.sh | sudo sh -s -- ins_…
```

It installs Docker when it is missing, installs the agent as the `ops-agent` systemd service and connects it. `ops servers` now shows `web-1 online`. The web UI shows the same under **Servers**, live.

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

Next: [Deploy](/docs/guides/deploy) explains what ops detects and how to configure it.
