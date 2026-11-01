---
title: "Self-hosting"
description: "Free forever: run all of ops yourself, the same product we host"
order: 3
---

**Free forever.** All of ops is open source under AGPL-3.0, the control plane too. Run it on your own server and you get exactly the product we host: every feature, unlimited servers, apps and members, no license key and no account with us.

| | Self-hosted | Hosted |
| --- | --- | --- |
| Price | free forever | $29 a month or $290 a year |
| Features | all | all |
| Servers, apps, members | unlimited | unlimited |
| Setup | about an hour | two minutes |
| Control plane | on your own server | ours, nothing to run |
| Updates | you pull and rebuild | the day they ship |
| GitHub App, email | you set them up | ready to use |
| Backups of the control plane | yours to run | every day, by us |
| Support | GitHub issues | email, from the team |
| goilerplate | not included | free with yearly, worth $99 |
| Where your apps and data are | your servers | your servers |

You need nothing from us, not even a license key. Prefer not to run it? Our hosted control plane is the same code.

## What is different

Only billing. With `BILLING=false` there are no prices, no checkout and no billing page, and every workspace gets unlimited servers, apps and members. Everything else is the code we run: the web UI, the API, MCP, deploys, backups, data, GitHub, teams and the activity.

## Where to run it

On its own small server, not on a server with your apps. The agent on an app server takes ports 80 and 443 for your apps, and a control plane apart from them keeps working when an app server fails or is broken into: you can still restore, roll back and see what happened. It needs no SSH key to your app servers, since their agents dial out to it.

## What you need

- A small Linux server with Docker for the control plane, reachable over HTTPS at your own domain.
- An S3-compatible bucket for the files of the control plane, such as avatars.
- Cloudflare Email Sending for sign-in links and invitations.
- Optional: your own GitHub App for GitHub deploys, see [GitHub](/docs/guides/github).

## Run it

Build the image from the repo:

```bash
git clone https://github.com/axadrn/ops
cd ops
docker build --build-arg VERSION=v0.1.0 -t ops-controlplane .
```

Start it with its settings in an env file and its SQLite database on a volume:

```bash
docker run -d --name ops --restart unless-stopped \
  -p 8090:8090 -v ops-data:/app/data --env-file ops.env \
  ops-controlplane
```

The settings in `ops.env`:

```bash
APP_ENV=production
APP_URL=https://ops.example.com
BILLING=false
DB_CONNECTION=./data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
# openssl rand -base64 32. Keep it: it seals the secrets of your apps.
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

`controlplane/.env.example` in the repo lists every setting. Put HTTPS in front with the proxy you like, pointed at port 8090.

## Use it

Sign up at your `APP_URL`, then point the CLI at it:

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

## Updates

Pull the repo, build the image again and start the new one with the same volume. The database migrates itself on start.
