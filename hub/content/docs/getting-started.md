---
title: "Get started"
description: "From a fresh server to a live app, in four steps"
group: "Start"
order: 2
---

moin puts your app on your own server. It keeps your data safe. New to the words? Read [moin in five words](/docs/moin-in-five-words).

Install the CLI on your Mac or Linux computer:

```bash
curl -fsSL https://moin.example.com/cli.sh | sh
```

Then four steps, and your app is live.

## 1. Log in

```bash
moin login --url https://moin.example.com
```

Your browser opens and shows a code. Check that it is the same code as in your terminal. Press **Approve**.

No account yet? The same page signs you up, with GitHub or your email.

## 2. Add a server

A server is your computer on the internet. Any Linux server works, amd64 or arm64, at any provider. You need to log in to it as root.

```surfaces
Web: **Servers**, **Add server**
CLI: moin servers add web-1
TUI: opens the web: `w`, then `a`
API: POST /api/v1/servers
MCP: add_server
```

moin prints a command. Run it on the server, as root, within one hour:

```bash
curl -fsSL https://moin.example.com/install.sh | sh -s -- ins_…
```

It installs Docker if it is missing. It starts the daemon, the little helper that only calls out to moin. Now `moin servers` shows the server as `online`.

## 3. Add your bucket

Your bucket is your own box on the internet. Your backups and uploads go there, never to us.

Any S3-compatible one works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner or Exoscale. Make a bucket, and a key that may read, write and delete.

```surfaces
Web: **Settings**, **Bucket**, **Add bucket**
CLI: moin buckets add default \
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --bucket <bucket-name> \
  --access-key <access-key>
TUI: opens the web: `w`, then `k`
API: POST /api/v1/buckets
MCP: add_bucket
```

The CLI asks for the secret key. For AWS S3, leave the endpoint out and set the region. [Bucket](/docs/guides/bucket) has the details.

## 4. Deploy

A deploy puts a new version of your app live. Go to the folder of your app and type:

```bash
moin deploy
```

The app does not exist yet, so moin asks first. Answer `y`.

```text
? App my-app does not exist. Create it? [y/N] y
Created app my-app on web-1
Uploading 42 files to your bucket (18.3 KB)
Deploy v1 of my-app started
  detected go, service web on port 8080
  v1 is live
✓ moin. my-app is live at https://my-app.203-0-113-7.sslip.io
```

You need no Dockerfile. moin builds Node, Bun, Python, Ruby, PHP, Go and more. It also uses your `Dockerfile` or `compose.yml` if you have one.

The app is online at once at its [instant address](/docs/guides/domains#instant-address), with HTTPS and a password. `moin instant --show` prints both.

This page deploys from your folder. You can also deploy from GitHub or from CI. It all comes down to [three questions](/docs/guides/deploy#three-questions).

## Good to know

- `moin` goes to `/usr/local/bin`, or to `~/.local/bin` when that is not writable. Then add `export PATH="$HOME/.local/bin:$PATH"` to `~/.zshrc` or `~/.profile`.
- No browser on this computer? Open the address `moin login` prints on any other device.
- Not root on the server? Put `sudo` before `sh`.
- More than one server? Use `moin deploy --server web-1`.
- A deploy failed? Its log ends with the reason. `moin deploys v1` shows it again.
- On a self-hosted hub, only admins and invited teammates sign up: [Who signs in](/docs/self-hosting#who-signs-in).
- The instant address needs ports 80 and 443. Behind a Cloudflare Tunnel, [add a domain](/docs/guides/domains) instead.

## Next

- [Why moin](/docs/why-moin)
- [Palette and keys](/docs/reference/palette): `⌘K` finds everything.
- [Catalog](/docs/guides/catalog): n8n, Umami, Open WebUI and more in one click.
- [Backups](/docs/guides/backups) and [Security](/docs/guides/security).
