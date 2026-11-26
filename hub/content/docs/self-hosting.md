---
title: "Self-hosting"
description: "Run your own moin hub. Free forever. The same product we host."
group: "Start"
order: 5
---

Self-hosting means you run the hub on your own server instead of using ours. You get the product we host. Every feature. Unlimited servers, apps and teammates. No license key. No account with us.

## Install

Point your domain at the server with an A record. Then run this as root on a fresh Ubuntu or Debian server:

```bash
curl -fsSL https://raw.githubusercontent.com/axadrn/moin/main/hub/deploy/install.sh | sh -s -- --domain moin.example.com --email you@example.com
```

It installs Docker. It writes the settings. It starts the hub with your account in it and serves it on your domain with HTTPS. At the end it prints a recovery key. Open the hub, enter the key, and you are in.

Then:

1. Click **Create GitHub App** at the top. You and your team sign in and deploy with GitHub. [Your own GitHub App](#your-own-github-app)
2. Add a passkey under **Settings**, **Account**, **Security**.
3. Point the CLI at it: `moin login --url https://moin.example.com`

The rest of these docs apply as they are.

### Which way

Pick one. Add its flags to the command above.

| Way | Flags | Pick it when |
| --- | --- | --- |
| [One server](#one-server) | none | You want the quick start. One machine for everything. |
| [Hub only](#hub-only) | `--hub-only` | You want full separation. A hacked hub cannot touch the machine your apps run on. |
| [With a tunnel](#with-a-tunnel) | `--hub-only --tunnel-token <token>` | You want no open port on the hub's server. |
| [Your own proxy](#your-own-proxy) | `--hub-only --no-proxy` | You run a proxy already. |

```text
one server      [ Traefik · hub · daemon · your apps ]

hub only        [ Caddy · hub ]        ◄── the daemons of your app servers call it
with a tunnel   [ cloudflared · hub ]  ◄── the same, and no port is open
your own proxy  [ your proxy · hub ]   ◄── the same, with the proxy you know
```

The hub never gets Docker. With `--hub-only`, nothing next to it gets Docker either: Caddy and cloudflared only pass requests on. On one server, the hub shares the machine with the daemon and its Traefik. Both use Docker, because they run your apps.

Run the install command again any time, with the same flags. It keeps your settings and data, and it fetches the newest deploy files.

### One server

The hub shares the machine with your apps. The daemon's Traefik serves the hub on your domain, like any app. It is the easy start. But if the server fails, the hub goes with it.

### Hub only

The hub runs on its own server, with Caddy in front of it. Caddy takes ports 80 and 443 and gets a certificate from Let's Encrypt. It has no access to Docker. Your app servers call the hub.

A hub apart from your apps keeps working when an app server fails. You can still restore, roll back and see what happened.

At the end the install prints the command for your first app server, `web-1`. Run it there, as root.

### With a tunnel

A Cloudflare Tunnel carries all traffic. No port is open, and you need no A record.

1. In Cloudflare, open **Zero Trust**, **Networks**, **Tunnels**, and create a tunnel.
2. Give it a public hostname: your domain, with the service `http://hub:8090`.
3. Copy its token, and install with `--hub-only --tunnel-token <token>`.

`cloudflared` gets only this one setting, the token.

### Your own proxy

Install with `--hub-only --no-proxy`. The hub answers on `127.0.0.1:8090`, on this machine only. Point your proxy at it. It serves HTTPS on your domain and sends the visitor's address in `X-Forwarded-For`, as Caddy and nginx do.

### On your tailnet

The safest hub is one the internet cannot reach:

1. Install with `--hub-only --no-proxy` and the server's tailnet name as the domain: `--domain <server>.<tailnet>.ts.net`.
2. Run `sh /opt/moin/setup.sh --tailscale`, then `tailscale serve --bg 8090`. It serves the hub at `https://<server>.<tailnet>.ts.net`.
3. Put each server on the tailnet with `tailscale up` before you install the daemon. Your computer too, for the web, the CLI and the TUI.

Your apps stay public through their own tunnel or ports 80 and 443. GitHub and CI outside your tailnet cannot reach the hub, so **Deploy every push** and deploy hooks do not work. Deploy from the web, the CLI or a CI runner on your tailnet.

## How moin.run runs

moin.run is a hub like yours. It runs the tunnel way, with the same files you get.

- It runs alone on its own server at Hetzner. Nothing else runs there. No app, no daemon.
- No port is open. Web traffic comes through Cloudflare Tunnel, and we reach the server only over Tailscale.
- Its settings live in 1Password. Every deploy fills them in fresh.
- Every deploy runs the same `update.sh` as yours. The new hub proves itself first, on a copy of the database.

[How a hub updates](/docs/reference/shipping#how-a-hub-updates)

## Good to know

- Keep a copy of `SECRETS_KEY`. It seals the Cloudflare token, the alert targets and the keys of the GitHub App the hub keeps. Without it you enter them again, and the hub starts only with the old key back or `GITHUB_APP_*` set. Env values, bucket keys and registry logins live on your servers, never in the hub.
- Email and a bucket can come later. Until email is set up, invitations go to the hub's log.
- A new key any time: [Recovery key](#recovery-key).

| | Self-hosted | Hosted |
| --- | --- | --- |
| Price | free forever | $7 a month or $49 a year, per workspace |
| Features; servers, apps, teammates | all; unlimited | all; unlimited |
| Setup | one command on one server | ready to use |
| Updates | one command, when a new version is out | by us, the day they ship |
| GitHub App | one click | ready to use |
| Email | you set it up | ready to use |
| Backup of the hub | yours to run | every day, by us |
| Support | GitHub issues | email |
| Your apps and data | your servers | your servers |

## What is different

- **Billing is off** (`BILLING=false`). Every workspace is unlimited.
- **It is private.** Your domain opens the sign-in page. Sign-up is closed ([Who signs in](#who-signs-in)).
- **The docs stay at `/docs`.**
- Everything else is the code we run.

## Settings

All settings are in one file: `/opt/moin/.env`. Only root reads it. Docker Compose reads it, and so does the hub. After a change, run `sh /opt/moin/update.sh`.

```bash
COMPOSE_FILE=compose.yml:compose.same-server.yml
DOMAIN=moin.example.com
APP_ENV=production
BILLING=false
# The first address is the admin. Only these may sign up.
ADMIN_EMAILS=you@example.com
DB_CONNECTION=./data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
# Seals the Cloudflare token, the alert targets and the GitHub App's keys. Keep a copy.
SECRETS_KEY=<from openssl rand -base64 32>
# The proxy in front of the hub comes from Docker's networks.
TRUSTED_PROXY_CIDRS=172.16.0.0/12,192.168.0.0/16
EMAIL_FROM=moin@moin.example.com
CLOUDFLARE_ACCOUNT_ID=
CLOUDFLARE_API_TOKEN=
S3_ENDPOINT=
S3_REGION=auto
S3_BUCKET=
S3_ACCESS_KEY=
S3_SECRET_KEY=
```

- **moin runs on HTTPS.** The hub's address, `APP_URL`, is always `https://` and your `DOMAIN`. You do not set it. On a tailnet or behind your own proxy, that proxy serves HTTPS on `DOMAIN` too.
- **A value with `$`, `#` or a space goes in single quotes**, like `S3_SECRET_KEY='a$b'`. Without them, Compose reads `$b` as a variable.
- **Files the settings point to**, like the key of a GitHub App, go in `/etc/moin-hub`. The hub sees them at `/run/moin-hub`.
- **The database** is in `/var/lib/moin-hub`. Only root reads it.

`hub/.env.example` lists every setting of the hub. The ones these docs use:

| Setting | What it does |
| --- | --- |
| `COMPOSE_FILE` | the files of your way, joined by `:`. One server: `compose.yml:compose.same-server.yml`. Hub only: `compose.yml:compose.caddy.yml`. With a tunnel: `compose.yml:compose.tunnel.yml`. Your own proxy: `compose.yml`. Every way needs its line: without one, `update.sh` stops. |
| `DOMAIN` | the domain of the hub. The hub's address, `APP_URL`, is `https://` and it. [Passkeys](#who-signs-in) belong to it. |
| `ADMIN_EMAILS` | the admins, separated by commas; the only ones who may sign up with GitHub without an invitation |
| `SECRETS_KEY` | seals the secrets the hub keeps itself: the Cloudflare token, the alert targets and the GitHub App's keys; keep a copy |
| `BILLING` | `false` turns off prices, checkout and the billing page |
| `SIGNUPS_OPEN` | `true` lets anyone sign up; off by default without billing, keep it off on a hub the internet can reach |
| `EMAIL_FROM`, `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN` | email through Cloudflare Email Sending |
| `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` | the bucket of the hub |
| `BACKUP_DATABASE` | backup of the hub's database; on by default in production once `S3_BUCKET` is set, `false` turns it off |
| `UPTIME` | [uptime](/docs/guides/uptime); on in production, off in development |
| `TUNNEL_TOKEN` | the token of your Cloudflare Tunnel; `cloudflared` gets only this one setting |
| `TRUSTED_PROXY_CIDRS` | where Traefik, Caddy, the tunnel or your proxy comes from: Docker's own networks, `172.16.0.0/12,192.168.0.0/16`. Gave Docker other address pools in `daemon.json`? Put those here. Without it every visitor counts as one address, and five wrong recovery keys lock everyone out for 15 minutes. |
| `OLD_URLS` | old domains the hub still answers on, see [A new domain](#a-new-domain) |
| `TELEMETRY` | `false` stops the count it sends us, see [What it sends us](#what-it-sends-us) |
| `UPDATE_CHECK` | `false` stops the daily check for a new version |
| `ROBOTS_NOINDEX` | asks search engines to stay away; on by default without billing |
| `UMAMI_WEBSITE_ID`, `UMAMI_HOST` | count visitors with your own Umami; off by default |
| `GITHUB_APP_*` | a GitHub App [made by hand](#by-hand); wins over the one the hub created |

### The files

They are in `/opt/moin`, from the repo's `hub/deploy/`:

| File | What it does |
| --- | --- |
| `compose.yml` | runs the hub |
| `compose.same-server.yml` | one server: the hub joins the daemon's Traefik |
| `compose.caddy.yml` | hub only: Caddy, for HTTPS |
| `compose.tunnel.yml` | with a tunnel: `cloudflared` |
| `update.sh` | starts the hub, and later updates it |
| `setup.sh` | Docker, security updates and the folders; with `--tailscale` SSH on your tailnet |

## Updates

```bash
sh /opt/moin/update.sh
```

It does three things:

1. It pulls the new version. The running hub keeps serving.
2. The new version starts once, on a copy of the database, with your settings. If that fails, it prints why and stops. The running hub stays.
3. Only then it switches. moin is away for a few seconds. Your apps keep running.

- Once a day the hub asks GitHub for the newest version. When one is out, the admins see a note at the top of the web: the version, what is new, a link to the [changelog](/changelog).
- Read the release notes first. A step you do by hand is in the notes under **Upgrade**.
- Before the database changes, the hub keeps a copy next to it, like `/var/lib/moin-hub/backup-before-v0.3.0.db`. The last three stay.
- `moin version` shows the version and whether a newer one is out. You only get published versions, never work in between.
- `UPDATE_CHECK=false` stops the daily check.
- One exact version, for one run: `MOIN_IMAGE=ghcr.io/axadrn/moin:<version> sh /opt/moin/update.sh`.
- New deploy files come with the install command. Run it again, with the same flags.
- The daemons update themselves to the version of the hub, one server at a time, never during a deploy: [How a daemon updates](/docs/reference/shipping#how-a-daemon-updates).
- Your hub runs the image `ghcr.io/axadrn/moin:stable`, the newest release. Which versions exist and when they come: [How moin ships](/docs/reference/shipping).

### Go back to an older version

```bash
cd /opt/moin
MOIN_IMAGE=ghcr.io/axadrn/moin:v1.0.0-beta.36 docker compose up -d
```

This skips the check on a copy on purpose: older versions do not have it. The next `sh update.sh` brings the newest version again.

Did the newer version change the database? Then the older one may not start on it. Put back the copy from before the update. You lose what changed since.

```bash
cd /opt/moin
docker compose stop hub
cp /var/lib/moin-hub/backup-before-v1.0.0-beta.37.db /var/lib/moin-hub/app.db
rm -f /var/lib/moin-hub/app.db-wal /var/lib/moin-hub/app.db-shm
MOIN_IMAGE=ghcr.io/axadrn/moin:v1.0.0-beta.36 docker compose up -d
```

The copy is named after the version that changed the database.

### Coming from before v1.0.0-beta.37

Before v1.0.0-beta.37 the settings were in `/etc/moin-hub/env`, and an updater ran next to the hub. Now they are in `/opt/moin/.env`. Move them once, as root:

1. Copy the settings: `install -m 600 /etc/moin-hub/env /opt/moin/.env`
2. In `/opt/moin/.env`, add `DOMAIN=` with the host of your `APP_URL`, like `DOMAIN=moin.example.com`. Then delete the lines `APP_URL` and `SAME_SERVER`.
3. Add the `COMPOSE_FILE` line of your way:

   | You had | Add |
   | --- | --- |
   | `SAME_SERVER=true` | `COMPOSE_FILE=compose.yml:compose.same-server.yml` |
   | `SAME_SERVER=true` and `TUNNEL_TOKEN` | `COMPOSE_FILE=compose.yml:compose.same-server.yml:compose.tunnel.yml` |
   | `TUNNEL_TOKEN` | `COMPOSE_FILE=compose.yml:compose.tunnel.yml` |
   | your own proxy | `COMPOSE_FILE=compose.yml` |

4. Get the new files and start:
   - One server: run the install command again, with the same `--domain` and `--email`. It keeps your settings and data.
   - The hub on its own server:

     ```bash
     cd /opt/moin
     for file in setup.sh update.sh compose.yml compose.same-server.yml compose.caddy.yml compose.tunnel.yml; do
       curl -fsSL "https://raw.githubusercontent.com/axadrn/moin/main/hub/deploy/$file" -o "$file"
     done
     sh update.sh
     ```

5. Clean up: `rm -rf /etc/moin-hub/env /opt/moin/updater.sh /var/lib/moin-hub/update`

The old updater goes away by itself with the update. Ran your hub from a git clone, in its `hub/deploy`? Stop it there first, with `docker compose --profile '*' down`, then start with step 1.

- **Update now** in the web of v1.0.0-beta.35 or beta.36 still takes you to v1.0.0-beta.37. The update after that needs the new place.
- Still have `/etc/moin-controlplane`, from before v1.0.0-beta.25? Run your old `update.sh` once first, with `MOIN_IMAGE=ghcr.io/axadrn/moin:v1.0.0-beta.35`. It moves the folders. Then start with step 1.
- Filling the settings from 1Password with `env.tpl` and `op-token` is gone. Put the values in `/opt/moin/.env` yourself. Then delete `/etc/moin-hub/env.tpl` and `/etc/moin-hub/op-token`.

## Move the hub to its own server

1. Turn on the hub's backup: set `S3_*`, then `sh /opt/moin/update.sh`. Use a bucket no other hub backs up to (folder `hub/litestream`).
2. Wait a few seconds. Then, on the old server, stop the hub: `cd /opt/moin && docker compose down`. Your apps keep running.
3. Copy `/opt/moin` and `/etc/moin-hub` to the new server. In `/opt/moin/.env` there, set `COMPOSE_FILE=compose.yml:compose.caddy.yml`. Keep the same `SECRETS_KEY` above all.
4. On the new server, run `sh /opt/moin/setup.sh`, then `sh /opt/moin/update.sh`. The hub restores its database from the bucket on its first start. Point the domain at it.
5. On the old server, run a new install command from `moin servers install-command <server>`. Its daemon now dials your domain, as the same server.
6. Once all works, on the old server: `rm -r /opt/moin /etc/moin-hub /var/lib/moin-hub`.

## Database backup

The hub's own SQLite database goes to its bucket with Litestream, every change within a second (`S3_BUCKET`, folder `hub/litestream`). On by default in production once `S3_BUCKET` is set; `BACKUP_DATABASE=false` turns it off.

The admins of the hub (`ADMIN_EMAILS`) see how it stands in **Settings**, **Hub backup**, in `moin version`, in `GET /api/v1/version` and in `get_version`: `● on · bucket <name> · last copy just now`, red when it is off or no copy reached the bucket for two minutes.

| File | What is in it | Backup |
| --- | --- | --- |
| `app.db` (the name in `DB_CONNECTION`) | users, apps, deploys, settings, the uptime state of each domain and every outage | every change, within a second |
| `history.db` | [metrics](/docs/guides/metrics) and [uptime](/docs/guides/uptime) per hour | none: it grows back, losing it costs old charts |

A hub that starts without a database restores the newest one from the bucket by itself. An empty bucket means a fresh start. So to rebuild a server: copy `/opt/moin` and `/etc/moin-hub` to it, then run `sh /opt/moin/setup.sh` and `sh /opt/moin/update.sh`.

## Your own GitHub App

People sign in with GitHub, and deploys come from GitHub, through one GitHub App of your hub. The hub creates it with you, in one click:

1. Sign in with the [recovery key](#recovery-key). At the top of the web it says "No GitHub App yet."
2. Click **Create GitHub App**. GitHub shows the new app, named like `moin-moin-example-com`. Change the name if GitHub says it is taken, then create it.
3. GitHub sends you back. The hub keeps the app, its keys sealed with `SECRETS_KEY`, and uses it at once. No restart.
4. Connect your GitHub under **Settings**, **Account**, **Security**, and give the app your repos with **Connect GitHub** on an app.

Only the admins in `ADMIN_EMAILS` see the button, and only while the hub has no app. The app asks GitHub for what moin uses and nothing more: Contents read, Commit statuses read and write, Metadata read, and Email addresses read to sign people in; the events Push and Installation.

The app is public on GitHub, so your team signs in with it and installs it on their own accounts and organizations. Sign-up stays as you set it: only invited people join, unless `SIGNUPS_OPEN` is on, and the hub checks who installed the app before a workspace uses an installation.

### Start over

1. Delete the app on GitHub: **Settings**, **Developer settings**, **GitHub Apps**, the app, **Advanced**, **Delete GitHub App**.
2. Remove it from the hub, and restart the hub:

   ```bash
   cd /opt/moin
   docker compose exec hub sqlite3 data/app.db 'DELETE FROM github_app'
   docker compose restart hub
   ```

3. Click **Create GitHub App** again, then **Connect GitHub** on each app.

A create that stopped halfway can leave an app on GitHub the hub never got. Delete it the same way, as in step 1. It also frees its name.

### By hand

Or create it on GitHub under **Settings**, **Developer settings**, **GitHub Apps**, with the permissions and events above. Callback URLs: first `https://<your domain>/app/github/setup`, then `https://<your domain>/auth/github/callback`, in that order, with **Request user authorization (OAuth) during installation** and **Redirect on update** ticked. Webhook URL: `https://<your domain>/webhooks/github`, with a secret of your own. Installable on **Any account**. Generate a private key and a client secret. Put the key at `/etc/moin-hub/github-app.pem`, add the rest to `/opt/moin/.env` and run `sh /opt/moin/update.sh`:

```bash
GITHUB_APP_ID=
GITHUB_APP_SLUG=
GITHUB_APP_PRIVATE_KEY_FILE=/run/moin-hub/github-app.pem
GITHUB_APP_WEBHOOK_SECRET=
GITHUB_APP_CLIENT_ID=
GITHUB_APP_CLIENT_SECRET=
```

`GITHUB_APP_*` wins over the app the hub created. Without the client ID and secret nobody signs in with GitHub, moin trusts the installation in the link GitHub sends back, and it says so in its log when it starts.

## Who signs in

Everyone signs up with GitHub, then signs in with GitHub or a [passkey](/docs/guides/account#add-a-passkey). No passwords, no links by email.

- **You, first.** The install made your account. Sign in with the [recovery key](#recovery-key) it printed, then **Connect GitHub** under **Settings**, **Account**, **Security**. Or, once the GitHub App is set up, sign in with a GitHub account whose verified primary email is your address in `ADMIN_EMAILS`.
- **Your team.** Invite teammates. The invitation link lets them in with any GitHub account, under the invited address.
- **Nobody else.** If `ADMIN_EMAILS` is empty nobody can create the first account, and the log says so when the hub starts.
- **Passkeys need a domain with HTTPS.** A passkey belongs to the hub's domain, and browsers make one only over HTTPS. With an IP address as the domain, the hub starts with passkeys off and says why in its log.

### Recovery key

A key from the server signs one account in, once, within an hour. It is the way in before the GitHub App is set up, and for someone who lost their GitHub account or passkey.

```bash
cd /opt/moin
docker compose exec hub ./main recovery-key you@example.com
```

It prints the key and where to enter it, `https://<your domain>/auth/key`. A new key replaces the one before. The key goes to your terminal only, never to a log or a link. It resets every way in: it signs the account out everywhere, ends its connections and CI tokens, disconnects its GitHub, deletes its passkeys and drops an email change still waiting for its link. After it, connect GitHub and add a passkey again under **Settings**, **Account**, **Security**. A hub without the GitHub App asks for the key on its sign-in page, next to **Sign in with a passkey**.

A self-hosted hub has no Admin. Its admins, the addresses in `ADMIN_EMAILS`, get the note about a new version at the top of the web ([Updates](#updates)) and see the hub backup in **Settings** ([Database backup](#database-backup)). Your team is in **Settings**, **Team**.

## A new domain

Set `DOMAIN` to the new one and put the old one in `OLD_URLS`, like `OLD_URLS=https://old.example.com`. Then run `sh /opt/moin/update.sh`. A browser on the old domain goes on to the same page on the new one, daemons and the CLI move by themselves, and webhooks and API calls there keep working. A self-hosted hub does not show what still arrives there. Keep the old domain until your webhooks, CI and scripts call the new one, then it can go.

Passkeys stay with the old domain: a new domain ends every passkey. Everyone signs in with GitHub once and adds a new passkey. Without GitHub, make them a [recovery key](#recovery-key).

## What it sends us

Every five minutes a self-hosted hub tells us it runs, with a few counts:

```json
{"instance": "8f14e45f-ceea-467a-9575-6c6f2b2b8f1e", "version": "v0.1.0", "counts": {"servers": 3, "apps": 7, "deploys": 412}}
```

The instance ID is random, made on the first start. No name, domain, email, address or app data, and we do not keep the address it comes from. One that stops sending drops off within fifteen minutes. The code is in `hub/internal/service/telemetry.go`, and the log says so at start. `TELEMETRY=false` turns it off. The daily check for a new version carries nothing about you.
