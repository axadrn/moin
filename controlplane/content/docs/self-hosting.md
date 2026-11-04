---
title: "Self-hosting"
description: "Free forever: run all of moin yourself, the same product we host"
order: 3
---

**Free forever.** All of moin is open source under AGPL-3.0, the control plane too. Run it on your own server and you get exactly the product we host: every feature, unlimited servers, apps and members, no license key and no account with us.

| | Self-hosted | Hosted |
| --- | --- | --- |
| Price | free forever | $249 a year (save $99) or $29 a month, per workspace |
| Features | all | all |
| Servers, apps, members | unlimited | unlimited |
| Setup | one command on one server, about an hour on two | two minutes |
| Control plane | on your own server | ours, nothing to run |
| Updates | one click, when moin says a release is out | the day they ship, by us |
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
- Sign-up is closed. New accounts are only for the addresses in `ADMIN_EMAILS` and for teammates invited to a workspace. Anyone else sees "This moin is private. Ask its admin for an invitation."
- The docs stay at `/docs`. The terms and privacy pages of our hosted service are not there, and search engines are asked to stay away (`ROBOTS_NOINDEX` is on).

## Three ways to run moin

- **Hosted.** We run the control plane. Your servers keep your apps and data, with no open port for us.
- **One server.** The control plane and your apps on one machine, in one command. The way to start.
- **Two servers.** The control plane on its own small server. Recommended once it matters: an app that is broken into cannot reach the control plane, and you can still restore, roll back and see what happened.

## One server

Point a domain at the server with an A record, then run this as root on a fresh Ubuntu or Debian server:

```bash
curl -fsSL https://raw.githubusercontent.com/axadrn/moin/main/controlplane/deploy/install.sh | sh -s -- --domain moin.example.com --email you@example.com
```

It takes a few minutes and does it all:

1. Installs Docker and security updates.
2. Writes the settings to `/etc/ops-controlplane/env`, with a new `SECRETS_KEY`. Keep a copy of that key: it seals the secrets of your apps.
3. Starts the control plane, and makes your account, a workspace and this server in it.
4. Installs the agent, which reaches the control plane on this machine and starts the proxy for ports 80 and 443.
5. Serves the control plane on your domain, with a certificate from Let's Encrypt, like any app.

At the end it prints a sign-in link. Email and a bucket can come later: until email is set up, sign-in links go to the control plane's log, and this prints a new one:

```bash
docker compose -f /opt/ops/compose.yml exec controlplane ./main sign-in-link
```

Fill in `CLOUDFLARE_*` for email and `S3_*` with `BACKUP_DATABASE=true` for the control plane's own backup, then run `sh /opt/ops/update.sh`. Run the install command again any time: it keeps your settings and data.

### Move from one server to two

1. Turn on the control plane's backup: fill in `S3_*`, set `BACKUP_DATABASE=true`, run `sh /opt/ops/update.sh`. Use a bucket no other control plane backs up to: its backup lives in the folder `controlplane/litestream`.
2. Wait a few seconds, so the last change is in the bucket. Then, on the old server, stop the control plane and its updater: `docker compose -f /opt/ops/compose.yml --profile '*' down`. Your apps keep running, and the proxy still serves them.
3. On the new server, follow [Run it](#run-it) below with the same settings, without `SAME_SERVER`. The same `SECRETS_KEY` matters most: it opens your apps' secrets. The control plane restores its database from the bucket on its first start: your account, workspaces, apps and API tokens are there. Point the domain at the new server.
4. The agent on the old server still looks for the control plane on its own machine. Get a new install command with `moin servers install-command <server>` and run it on the old server: the agent now dials your domain, as the same server. Your apps keep running.
5. Once all works, remove the old settings from the old server: `rm -r /opt/ops /etc/ops-controlplane /var/lib/ops-controlplane`.

## Where to run it

On its own small server, not on a server with your apps, once it matters. The agent on an app server takes ports 80 and 443 for your apps, and a control plane apart from them keeps working when an app server fails or is broken into: you can still restore, roll back and see what happened. It needs no SSH key to your app servers, since their agents dial out to it.

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
| `updater.sh` | Runs next to it and does **Update now**, see [Updates](#updates) |

**1. Prepare the server**, as root:

```bash
git clone https://github.com/axadrn/moin
cd moin/controlplane/deploy
sh setup.sh
```

**2. Write the settings** into `/etc/ops-controlplane/env`:

```bash
APP_ENV=production
APP_URL=https://moin.example.com
BILLING=false
# You: the first admin. Sign up with this address.
ADMIN_EMAILS=you@example.com
DB_CONNECTION=./data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
# openssl rand -base64 32. Keep a copy: it seals the secrets of your apps.
SECRETS_KEY=
EMAIL_FROM=hello@example.com
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

### Your own GitHub App

For deploys from GitHub, the control plane needs a GitHub App of its own. Create it on GitHub under **Settings**, **Developer settings**, **GitHub Apps**:

1. **Permissions:** Contents read, Commit statuses read and write. **Events:** Push and Installation.
2. **Callback URL:** `{APP_URL}/app/github/setup`. Tick **Request user authorization (OAuth) during installation** and **Redirect on update**.
3. **Webhook URL:** `{APP_URL}/webhooks/github`, with a secret of your own.
4. Generate a private key and a client secret.

Put the key next to the settings as `/etc/ops-controlplane/github-app.pem`, add the rest to the settings and run `update.sh` again:

```bash
GITHUB_APP_ID=
GITHUB_APP_SLUG=
GITHUB_APP_PRIVATE_KEY_FILE=/run/ops-controlplane/github-app.pem
GITHUB_APP_WEBHOOK_SECRET=
GITHUB_APP_CLIENT_ID=
GITHUB_APP_CLIENT_SECRET=
```

With the client ID and secret, moin asks GitHub who installed the app, and connects an installation only for someone whose GitHub account may manage it. They also let **Connect GitHub** work when the app is already installed: moin signs the user in to GitHub and connects the installations they have. Without them, moin trusts the installation in the link GitHub sends back and says so in its log when it starts.

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
moin login --url https://moin.example.com
```

The install line for a server, the CLI, the terminal UI and MCP all talk to your control plane from then on. The rest of these docs apply as they are.

## What it sends us

Every five minutes a self-hosted control plane tells us that it runs, with a few counts and nothing else:

```json
{"instance": "8f14e45f-ceea-467a-9575-6c6f2b2b8f1e", "version": "v0.1.0", "counts": {"servers": 3, "apps": 7, "deploys": 412}}
```

The instance ID is random and made once, on the first start. There is no name, domain, email, address or data of your apps in it, and we do not keep the address it comes from. The numbers add up to what the landing page shows as running on moin right now; a control plane that stops sending drops off within fifteen minutes. The code is in `controlplane/internal/service/telemetry.go`, and the control plane says so in its log when it starts.

Turn it off with one setting:

```bash
TELEMETRY=false
```

Once a day it also asks GitHub for the newest release, see [Updates](#updates). That request carries nothing about you.

## Database backup

The control plane keeps its own SQLite database safe the way it keeps your apps' databases: every change goes to its bucket within seconds (`S3_BUCKET`, folder `controlplane/litestream`), with Litestream. In production this is on by default; `BACKUP_DATABASE=false` turns it off.

The data folder holds two files:

| File | What is in it | Backup |
| --- | --- | --- |
| `app.db` (the name in `DB_CONNECTION`) | everything that must not get lost: users, apps, deploys, settings, the state of each uptime check and every outage | every change, in your bucket within a second |
| `history.db` | everything that is a chart over time: [metrics](/docs/guides/metrics) and [uptime](/docs/guides/uptime) per hour | none |

The rule: what an alert or a decision depends on goes in the database; what is a chart goes in `history.db`. History grows back by itself, so losing `history.db` costs old charts, nothing else. The database stays small, its backup cheap and a restore fast.

## Uptime checks

The control plane checks every domain of every app once a minute, like the hosted one ([Uptime](/docs/guides/uptime)). In production this is on by default; `UPTIME_CHECKS=false` turns it off. In development it is off, so no real domain gets called, and `UPTIME_CHECKS=true` turns it on.

When the control plane starts and finds no database, it restores the newest one from the bucket by itself. An empty bucket means a fresh start.

## Updates

**The control plane.** Once a day it asks GitHub for the newest release. When one is out, the admins in `ADMIN_EMAILS` see a note at the top of the web UI: the version, what is new in one line, and a link to the release notes. Then:

- Click **Update now**. Or run `moin update` in the CLI, or `u` on the settings tab of the TUI.
- Or run `sh update.sh` on the server, as before.

moin is away for about a minute. Your apps keep running. Before the database changes, moin keeps a copy of it next to it, like `/var/lib/ops-controlplane/backup-before-v0.3.0.db`; the last three stay. If the new version does not come up within two minutes, the old one starts again, and the note says why.

`moin version` shows the version of the control plane and says when a newer one is out. You only get releases, with their notes in the [changelog](/changelog), never work in between.

**Update now** needs the updater, a small helper next to the control plane. It is the only part with access to Docker. moin itself never touches Docker: someone who breaks into it can at most ask for an official moin release, never run their own code on your server. A control plane set up before it gets it once: `git pull` in the repo, then `sh update.sh`.

To stop asking GitHub, set `UPDATE_CHECK=false`. Then update with `sh update.sh` when you like.

**The agents** update themselves to the version of the control plane, one server at a time, never during a deploy. Your apps keep running. See [Servers](/docs/guides/servers#updates).

## From ops to moin

A control plane installed as ops moves once, the next time you run the new `update.sh`. Get the new deploy files, then run it:

```bash
mkdir -p /opt/moin && cd /opt/moin
for file in setup.sh compose.yml compose.same-server.yml update.sh updater.sh; do
  curl -fsSL https://raw.githubusercontent.com/axadrn/moin/main/controlplane/deploy/$file -o $file
done
sh update.sh
```

It stops the old control plane, copies `/etc/ops-controlplane` to `/etc/moin-controlplane` and `/var/lib/ops-controlplane` to `/var/lib/moin-controlplane`, and starts it from there with the image `ghcr.io/axadrn/moin`. The control plane is away for the copy. The old folders stay as they were: remove them once moin runs. The agent on the same server moves by itself, see [Servers](/docs/guides/servers#from-ops-to-moin).

Until December 4, 2026 the old install keeps working without the move: `ghcr.io/axadrn/ops-controlplane` gets every release, `OPS_IMAGE` and `OPS_PULL` still work, and settings that point to `/run/ops-controlplane` still find their files.

**The admin page.** `/admin` is for the addresses in `ADMIN_EMAILS` only: users, sessions, operator activity and **Update now**. Nobody else sees it. Your control plane counts only its own people; it never sees anyone else's.

**A new address.** When the control plane moves to a new address, set `APP_URL` to it and put the old one in `OLD_URLS`, like `OLD_URLS=https://ops.example.com`. The control plane answers on both: a browser on the old address goes on to the same page on the new one, and agents and the CLI move to it by themselves. Webhooks and API calls on the old address keep working. Admins see under **Admin**, **Old address**, or `GET /api/v1/admin/old-address`, what still arrives there, by day and kind; after 14 quiet days it can go.

## Move or rebuild

When the server dies or you move: prepare a new server with `setup.sh`, put your settings in `/etc/ops-controlplane/` again and run `update.sh`. The database comes back from the bucket by itself. With a tunnel, nothing else changes: the tunnel connects from the new server and your domain follows.
