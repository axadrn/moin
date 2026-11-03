---
title: "Getting Started"
description: "From a fresh server to a live app in five minutes"
order: 2
---

## What you need

- **An account on ops.** Sign up on the hosted control plane, or [host it yourself](/docs/self-hosting).
- **A Linux server** that you can log in to as root, on amd64 or arm64. Rent one at any provider, such as Hetzner.
- **A bucket**, an online storage box at Cloudflare R2, AWS S3, Backblaze B2, Hetzner or Exoscale. [Your bucket](/docs/guides/bucket) explains why ops needs one.
- **Your app in a folder**, with a `compose.yml`, a `Dockerfile` or a `go.mod` in it. [Deploy](/docs/guides/deploy#deploy-from-a-repo) says what ops does with each.
- **A domain**, if people should reach the app in a browser. You can add it at the end.

Not sure what a word means? [How ops works in 2 minutes](/docs#how-ops-works-in-2-minutes) explains each one.

## 1. Sign up

Open the control plane in your browser and press **Continue with GitHub**. No GitHub account, or a self-hosted control plane without GitHub sign-in? Enter your email instead, and ops sends you a sign-in link.

The first time, ops asks for two things:

- **Your name**, the one your team sees.
- **Workspace name**, your team's space for servers, apps and the bucket.

With GitHub both come filled in from your GitHub profile, such as "Ada Lovelace" and "Ada Lovelace's Workspace", so you press **Continue** and are done. With email you type them. You can change both later in the settings. When a teammate invited you, ops asks only for your name, and you join their workspace.

On a self-hosted control plane, only the admins and invited teammates sign up: see [Who signs in](/docs/self-hosting#who-signs-in).

## 2. Install ops on your computer

On a Mac or Linux:

```bash
curl -fsSL https://ops.example.com/cli.sh | sh
```

It puts `ops` in `/usr/local/bin`, or in `~/.local/bin` when that is not writable. If it says to add `~/.local/bin` to your PATH, add this line to `~/.zshrc` or `~/.profile` and open a new terminal:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

The commands on these pages name the control plane you read them on.

## 3. Create a token

The CLI signs in with a personal API token: a long password just for the CLI, scripts and AI agents.

1. On the web, open **Settings** in the sidebar, then the **Tokens** tab.
2. Press **Create token**.
3. Give it a name, such as the name of your computer.
4. Leave **Access** at **Read and write**. The CLI needs it to add servers and deploy.
5. Pick how long it works. When it ends, make a new one and log in again.
6. Press **Create token** and copy the token. ops shows it only once.

The token belongs to your workspace. Teammates you invite later work in the same one. [Account and tokens](/docs/guides/account#create-an-api-token) has more.

## 4. Log in

```bash
ops login --url https://ops.example.com
```

Paste the token when it asks. `ops login --url https://ops.example.com --token <token>` works without the prompt.

## 5. Add a server

Pick a name for the server, such as `web-1`. ops gives you one command to run on it.

```surfaces
Web: **Servers**, **Add server**
CLI: ops servers add <server-name>
TUI: on the servers tab, `a`
API: POST /api/v1/servers
MCP: add_server
```

Log in to your server as root, for example with `ssh root@<server-ip>`. Then run the command ops printed, within the next hour. It looks like this:

```bash
curl -fsSL https://ops.example.com/install.sh | sh -s -- ins_…
```

Not logged in as root? Put `sudo` before `sh`.

It installs Docker when it is missing, which takes a few minutes. Then it installs the agent as the `ops-agent` service and connects it. `ops servers` now shows your server as `online`. The web shows the same under **Servers**, live. [Servers](/docs/guides/servers) has more.

## 6. Add your bucket

Your code and your backups go into your own bucket, never to us.

1. Create a bucket at your storage provider.
2. Create an access key for it that may read, write and delete. You get an access key and a secret key.
3. Find the endpoint: the web address of the storage. For Cloudflare R2 it is `https://<account-id>.r2.cloudflarestorage.com`. It must start with `https://`. For AWS S3, leave it empty and set the region of the bucket instead.

Then tell ops:

```surfaces
Web: **Bucket** in the sidebar, then **Save bucket**
CLI: ops backups bucket set \
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --bucket <bucket-name> \
  --access-key <access-key>
TUI: settings tab, **Bucket**, then `enter`
API: PUT /api/v1/backup-storage
MCP: set_backup_storage
```

The CLI asks for the secret key. ops writes a test file and deletes it, so a wrong key fails right away. [Your bucket](/docs/guides/bucket#add-it) has the details.

## 7. Deploy

Go to the folder of your app and run:

```bash
ops deploy
```

The first deploy creates the app, named after the folder, on your only server. If you have more than one server, add `--server <server-name>`. ops packs the files git knows, the agent on your server builds and starts them, checks that they stay up and prints the log as it goes:

```text
Created app <app-name> on <server-name>
Uploading 42 files to your bucket (18.3 KB)
Deploy v1 of <app-name> started
  detected go, service web on port 8080
  building
  …
  v1 is live
```

If the deploy fails, the log ends with the reason. Read it again any time with `ops deploys v1`. [Deploy](/docs/guides/deploy) explains what ops detects and how to change it.

## 8. Open it

Your app runs, but nobody can reach it yet. Give it a domain:

```surfaces
Web: the app page, **Domains**, **Add domain**
CLI: ops domains add <your-domain>
TUI: on the app, `o`, then type the domain
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

ops shows the address of your server and the DNS record to set. Set it where you bought the domain, in its DNS settings: an **A record** that points the domain at the server's address. DNS can take a few minutes. `ops domains` shows when the domain points at your server, and then the app has HTTPS. [Domains and access](/docs/guides/domains) has the details.

## What next

- Settings and secrets for your app: [Env vars](/docs/guides/env).
- Your database, backed up every second: [Backups](/docs/guides/backups).
- Close every port of the server: [Security](/docs/guides/security).
- Deploy every push to GitHub: [GitHub](/docs/guides/github).
- Bring your team: [Team and activity](/docs/guides/team).
