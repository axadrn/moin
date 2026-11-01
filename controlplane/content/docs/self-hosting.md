---
title: "Self-hosting"
description: "Free forever: run all of ops yourself, the same product we host"
order: 3
---

**Free forever.** All of ops is open source under AGPL-3.0, the control plane too. Run it on your own server and you get exactly the product we host: every feature, unlimited servers, apps and members, no license key and no account with us.

| | Self-hosted | Hosted |
| --- | --- | --- |
| Price | free forever | $249 a year (save $99) or $29 a month |
| Features | all | all |
| Servers, apps, members | unlimited | unlimited |
| Setup | about an hour | two minutes |
| Control plane | on your own server | ours, nothing to run |
| Updates | you pull and rebuild | the day they ship |
| GitHub App, email | you set them up | ready to use |
| Backups of the control plane | yours to run | every day, by us |
| Support | GitHub issues | email, from the team |
| Where your apps and data are | your servers | your servers |

You need nothing from us, not even a license key. Prefer not to run it? Our hosted control plane is the same code.

**It is private.** A self-hosted control plane has no landing page and no sign-up for strangers: only admins (`ADMIN_EMAILS`) and teammates they invite sign in.

## What is different

Billing, and who gets in. With `BILLING=false` there are no prices, no checkout and no billing page, and every workspace gets unlimited servers, apps and members. Everything else is the code we run: the web UI, the API, MCP, deploys, backups, data, GitHub, teams and the activity.

And it is private:

- No landing page. Your `APP_URL` opens the sign-in page, or your workspace when you are signed in.
- Sign-up is closed. New accounts are only for the addresses in `ADMIN_EMAILS` and for teammates invited to a workspace. Anyone else sees "This ops is private. Ask its admin for an invitation." There is no waitlist.
- The docs stay at `/docs`. The terms and privacy pages of our hosted service are not there, and search engines are asked to stay away (`ROBOTS_NOINDEX` is on).

## Where to run it

On its own small server, not on a server with your apps. The agent on an app server takes ports 80 and 443 for your apps, and a control plane apart from them keeps working when an app server fails or is broken into: you can still restore, roll back and see what happened. It needs no SSH key to your app servers, since their agents dial out to it.

## What you need

- A small Linux server (Ubuntu or Debian) for the control plane, and a domain for it.
- An S3-compatible bucket for the files of the control plane.
- Cloudflare Email Sending, for sign-in links and invitations.
- Optional: a Cloudflare Tunnel, so no web port has to be open.
- Optional: your own GitHub App for GitHub deploys, see [GitHub](/docs/guides/github).

## Run it

Three files do the work. They are in the repo, in `controlplane/deploy/`:

| File | What it does |
| --- | --- |
| `setup.sh` | Prepares the server once: Docker, security updates, and with `--tailscale` SSH on your tailnet |
| `compose.yml` | Runs the control plane, and with a tunnel token also `cloudflared` |
| `update.sh` | Starts the control plane, and later updates it to the newest version |

**1. Prepare the server**, as root:

```bash
git clone https://github.com/axadrn/ops
cd ops/controlplane/deploy
sh setup.sh
```

**2. Write the settings** into `/etc/ops-controlplane/env`:

```bash
APP_ENV=production
APP_URL=https://ops.example.com
BILLING=false
# You: the first admin. Sign up with this address.
ADMIN_EMAILS=you@example.com
DB_CONNECTION=./data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
# openssl rand -base64 32. Keep a copy: it seals the secrets of your apps.
SECRETS_KEY=
EMAIL_FROM=ops@example.com
CLOUDFLARE_ACCOUNT_ID=
CLOUDFLARE_API_TOKEN=
S3_ENDPOINT=
S3_REGION=auto
S3_BUCKET=
S3_ACCESS_KEY=
S3_SECRET_KEY=
```

`controlplane/.env.example` lists every setting. Keep the file readable by root only: `chmod 600 /etc/ops-controlplane/env`.

**3. Start it:**

```bash
sh update.sh
```

The control plane listens on `127.0.0.1:8090`. Put it on your domain in one of two ways:

- **Cloudflare Tunnel, no open port.** Create a tunnel in Cloudflare (Zero Trust, Networks, Tunnels) with a public hostname for your domain and the service `http://controlplane:8090`. Add its token and the Docker networks as trusted proxies to the settings, then run `update.sh` again:

  ```bash
  TUNNEL_TOKEN=
  TRUSTED_PROXY_CIDRS=172.16.0.0/12
  ```

- **A proxy of your own** with HTTPS, such as Caddy, pointed at `127.0.0.1:8090`.

### Keep it private: run it on your tailnet

The safest control plane is one the internet cannot reach. Run it only on your Tailscale network, your private network of your own devices:

1. Prepare the server with `sh setup.sh --tailscale`. It joins your tailnet.
2. Serve the control plane on the tailnet, with HTTPS: `tailscale serve --bg 8090`. It is now at `https://<server>.<tailnet>.ts.net`. Use that as `APP_URL`, with no tunnel and no open port.
3. Put each server on the tailnet too, with `tailscale up`, before you install the agent. The agent then reaches the control plane.
4. Put your computer on the tailnet, for the web UI, the CLI and the TUI.

Your apps stay public: they get their visitors through a Cloudflare Tunnel or ports 80 and 443 on each server, never through the control plane.

GitHub and CI outside your tailnet cannot reach the control plane, so **Deploy every push** and deploy hooks do not work there. Deploy from the web, the CLI or a CI runner on your tailnet.

### Keep the settings in 1Password

Instead of writing passwords into `/etc/ops-controlplane/env`, keep them in 1Password:

1. Create a 1Password service account that may read one vault, and put its token on the server: `/etc/ops-controlplane/op-token`, readable by root only.
2. Write the settings as `/etc/ops-controlplane/env.tpl`, with references like `SECRETS_KEY={{ op://vault/item/field }}` in place of passwords.

`update.sh` fills them in from 1Password on every start. A changed password counts after the next `update.sh`.

### Who signs in

- **You, first.** Put your address in `ADMIN_EMAILS` and sign up with it, with a sign-in link. Add a password later under **Settings** if you like. `ADMIN_EMAILS` takes several addresses, separated by commas.
- **Your team.** Invite teammates to your workspace. They sign up with the invited address.
- **Nobody else.** Other addresses get no account.

If `ADMIN_EMAILS` is empty, nobody can create the first account, and the control plane says so in its log when it starts. Set it and run `update.sh` again.

If you do want anyone to sign up, set `SIGNUPS_OPEN=true`. Keep it off on a control plane the internet can reach.

### Analytics

Off by default. To count visitors with your own Umami, set `UMAMI_WEBSITE_ID` and, for a self-hosted Umami, `UMAMI_HOST` (like `umami.example.com`). The page then loads its script, and the content security policy allows it.

## Use it

Sign up at your `APP_URL` with your address from `ADMIN_EMAILS`, then point the CLI at it:

```bash
ops login --url https://ops.example.com
```

The install line for a server, the CLI, the terminal UI and MCP all talk to your control plane from then on. The rest of these docs apply as they are.

## What it sends us

Every five minutes a self-hosted control plane tells us that it runs, with a few counts and nothing else:

```json
{"instance": "8f14e45f-ceea-467a-9575-6c6f2b2b8f1e", "version": "v0.1.0", "counts": {"servers": 3, "apps": 7, "deploys": 412}}
```

The instance ID is random and made once, on the first start. There is no name, domain, email, address or data of your apps in it, and we do not keep the address it comes from. The numbers add up to what the landing page shows as running on ops right now; a control plane that stops sending drops off within fifteen minutes. The code is in `controlplane/internal/service/telemetry.go`, and the control plane says so in its log when it starts.

Turn it off with one setting:

```bash
TELEMETRY=false
```

## Database backup

The control plane keeps its own SQLite database safe the way it keeps your apps' databases: every change goes to its bucket within seconds (`S3_BUCKET`, folder `controlplane/litestream`), with Litestream. In production this is on by default; `BACKUP_DATABASE=false` turns it off.

When the control plane starts and finds no database, it restores the newest one from the bucket by itself. An empty bucket means a fresh start.

## Updates

Run `update.sh` again. It pulls the newest image and restarts; the database migrates itself on start.

## Move or rebuild

When the server dies or you move: prepare a new server with `setup.sh`, put your settings in `/etc/ops-controlplane/` again and run `update.sh`. The database comes back from the bucket by itself. With a tunnel, nothing else changes: the tunnel connects from the new server and your domain follows.
