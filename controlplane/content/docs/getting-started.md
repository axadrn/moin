---
title: "Getting Started"
description: "From a fresh server to a live app in five minutes"
order: 2
---

## What you need

- **An account on moin.** Sign up on the hosted control plane, or [host it yourself](/docs/self-hosting).
- **A Linux server** that you can log in to as root, on amd64 or arm64. Rent one at any provider, such as Hetzner.
- **A bucket**, an online storage box at Cloudflare R2, AWS S3, Backblaze B2, Hetzner or Exoscale. [Your bucket](/docs/guides/bucket) explains why moin needs one.
- **Your app in a folder**: with a `compose.yml`, a `Dockerfile` or a `go.mod`, or none of them for Node, Bun, Python, Ruby, PHP and more. [Deploy](/docs/guides/deploy#deploy-from-a-repo) says what moin does with each.
- **A domain**, if people should reach the app in a browser. You can add it at the end.

Not sure what a word means? [How moin works in 2 minutes](/docs#how-moin-works-in-2-minutes) explains each one.

## 1. Sign up

Open the control plane in your browser and press **Continue with GitHub**. No GitHub account, or a self-hosted control plane without GitHub sign-in? Enter your email instead, and moin sends you a sign-in link. GitHub finds your account by any verified email on your GitHub account, so an account made with your work address works too.

The first time, moin asks for two things:

- **Your name**, the one your team sees.
- **Workspace name**, your team's space for servers, apps and the bucket.

With GitHub both come filled in from your GitHub profile, such as "Ada Lovelace" and "Ada Lovelace's Workspace", so you press **Continue** and are done. With email you type them. You can change both later in the settings. When a teammate invited you, moin asks only for your name, and you join their workspace.

On a self-hosted control plane, only the admins and invited teammates sign up: see [Who signs in](/docs/self-hosting#who-signs-in).

## 2. Install moin on your computer

On a Mac or Linux:

```bash
curl -fsSL https://moin.example.com/cli.sh | sh
```

It puts `moin` in `/usr/local/bin`, or in `~/.local/bin` when that is not writable. If it says to add `~/.local/bin` to your PATH, add this line to `~/.zshrc` or `~/.profile` and open a new terminal:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

The commands on these pages name the control plane you read them on.

## 3. Log in

```bash
moin login --url https://moin.example.com
```

moin prints a code like `WDJB-MJHT` and opens your browser. Sign in there if you are not yet, check that the page shows the same code, and press **Approve**. The terminal says `Logged in as` with your name.

No browser on this machine, for example over SSH? Open the address moin prints on any other device, your phone too, and approve it there.

The login belongs to this computer and your workspace. It shows as **CLI on** and the computer's name under **Settings**, **Tokens**, and ends after 90 days without use. [Account and tokens](/docs/guides/account#log-in-the-cli) has more, and how CI logs in with a token.

## 4. Add a server

Pick a name for the server, such as `web-1`. moin gives you one command to run on it.

```surfaces
Web: **Servers**, **Add server**
CLI: moin servers add <server-name>
TUI: opens the web: `w`, then `a`
API: POST /api/v1/servers
MCP: add_server
```

Log in to your server as root, for example with `ssh root@<server-ip>`. Then run the command moin printed, within the next hour. It looks like this:

```bash
curl -fsSL https://moin.example.com/install.sh | sh -s -- ins_…
```

Not logged in as root? Put `sudo` before `sh`.

It installs Docker when it is missing, which takes a few minutes. Then it installs the agent as the `moin-agent` service and connects it. `moin servers` now shows your server as `online`. The web shows the same under **Servers**, live. [Servers](/docs/guides/servers) has more.

## 5. Add your bucket

Your code and your backups go into your own bucket, never to us.

1. Create a bucket at your storage provider.
2. Create an access key for it that may read, write and delete. You get an access key and a secret key.
3. Find the endpoint: the web address of the storage. For Cloudflare R2 it is `https://<account-id>.r2.cloudflarestorage.com`. It must start with `https://`. For AWS S3, leave it empty and set the region of the bucket instead.

Then tell moin:

```surfaces
Web: **Buckets** in the sidebar, then **Add bucket**
CLI: moin buckets add default \
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --bucket <bucket-name> \
  --access-key <access-key>
TUI: opens the web: `w`, then `k`
API: POST /api/v1/buckets
MCP: add_bucket
```

The CLI asks for the secret key. moin writes a test file and deletes it, so a wrong key fails right away. [Your bucket](/docs/guides/bucket#add-it) has the details.

## 6. Deploy

Go to the folder of your app and run:

```bash
moin deploy
```

The first deploy creates the app, named after the folder, on your only server. If you have more than one server, add `--server <server-name>`. moin packs the files git knows, the agent on your server builds and starts them, checks that they stay up and prints the log as it goes:

```text
Created app <app-name> on <server-name>
Uploading 42 files to your bucket (18.3 KB)
Deploy v1 of <app-name> started
  detected go, service web on port 8080
  building
  …
  v1 is live
✓ moin. <app-name> is live at https://<app-name>.<server-ip-with-dashes>.sslip.io
```

If the deploy fails, the log ends with the reason. Read it again any time with `moin deploys v1`. [Deploy](/docs/guides/deploy) explains what moin detects and how to change it.

## 7. Open it

Your app is already online at its [instant address](/docs/guides/domains#instant-address), with HTTPS and a password in front: `moin instant --show` prints both. On a server behind a Cloudflare Tunnel there is none, since it needs ports 80 and 443.

For your visitors, give it a domain:

```surfaces
Web: the app page, **Domains**, **Add domain**
CLI: moin domains add <your-domain>
TUI: on the app, `o`, `a`, then type the domain
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

moin shows the address of your server and the DNS record to set. Set it where you bought the domain, in its DNS settings: an **A record** that points the domain at the server's address. DNS can take a few minutes. `moin domains` shows when the domain points at your server, and then the app has HTTPS. [Domains and access](/docs/guides/domains) has the details.

## What next

- No code of your own? Install n8n, Umami, Open WebUI and 44 more in one click: [Catalog](/docs/guides/catalog).
- Settings and secrets for your app: [Env vars](/docs/guides/env).
- Your database, SQLite or Postgres, every change backed up and checked daily: [Backups](/docs/guides/backups).
- Close every port of the server: [Security](/docs/guides/security).
- Deploy every push to GitHub: [GitHub](/docs/guides/github).
- Run a migration or make the first admin: [Run a command](/docs/guides/exec).
- Hear when a site goes down, a backup stops or a disk fills up: [Alerts](/docs/guides/alerts).
- Bring your team: [Team and activity](/docs/guides/team).
