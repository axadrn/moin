---
title: "Deploy a Docker Compose app to a VPS"
description: "From a fresh VPS to your app live on HTTPS, step by step: Docker, your compose.yml, Caddy for certificates, and updates."
date: "2026-11-25"
---

Your app runs with `docker compose up` on your laptop. The same file can run it on a server for a few dollars a month. Here is the whole way, with nothing hidden.

## 1. Get a server

Any VPS with Ubuntu or Debian works. For one small app, 2 GB of memory is enough. Hetzner, DigitalOcean, Vultr: pick one close to your users.

Add your SSH key when you create it. Then:

```bash
ssh root@203.0.113.9
```

## 2. Install Docker

Docker's own script installs Docker and the compose plugin:

```bash
curl -fsSL https://get.docker.com | sh
docker compose version
```

## 3. Bring your app

Copy your compose file to the server:

```bash
scp compose.yml root@203.0.113.9:/opt/my-app/
```

Secrets go next to it in a `.env` file, never in the repo. Compose reads `.env` by itself.

## 4. Add HTTPS

[Caddy](https://caddyserver.com) gets certificates from Let's Encrypt on its own and renews them. Add it to your compose file:

```yaml
services:
  app:
    image: ghcr.io/acme/my-app:1.4.2
    env_file: .env

  caddy:
    image: caddy:2
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - caddy-data:/data

volumes:
  caddy-data:
```

And a `Caddyfile` next to it:

```
my-app.example.com {
  reverse_proxy app:8080
}
```

Point the DNS record of `my-app.example.com` to the server's IP. Caddy reaches the app by its service name. The app itself publishes no port.

## 5. Start it

```bash
cd /opt/my-app
docker compose up -d
docker compose logs -f
```

Open `https://my-app.example.com`. Done.

## 6. Update it

Change the image tag in `compose.yml`, then:

```bash
docker compose pull
docker compose up -d
```

Pin a version like `1.4.2` instead of `latest`. Then you know what runs, and going back is changing one line.

## What this does not do yet

This is a real deploy, and many apps run like this for years. But it leaves a few things to you:

- **Backups.** The database lives on one disk. See [A backup you never restored is not a backup](/blog/a-backup-you-never-restored).
- **Security.** Port 22 is open to the world. See [Your server needs no open port](/blog/your-server-needs-no-open-port).
- **Knowing it is down.** Nothing tells you when the app stops answering.
- **Going back.** If `1.4.3` breaks, you SSH in and fix it by hand.

## How moin does it

[moin](/) takes the same `compose.yml` and does the rest with one command, `moin deploy prod`: HTTPS from the first deploy, backups of SQLite and Postgres with a check every day that a copy restores, a watcher that tells you when the server is gone, and rollback to an earlier deploy. After the first deploy, public SSH closes.

[Get started](/docs/getting-started) takes a fresh server to a live app in about two minutes.
