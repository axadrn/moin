---
title: "compose.yml"
description: "What moin reads from compose.yml, and the compose file it runs, to take with you. It is plain Docker, and every moin setting is in moin."
group: "Apps"
order: 7
---

`compose.yml` is plain Docker. moin reads only Docker's own fields. It works the same with `docker compose up` on your laptop. `compose.yaml` and `docker-compose.yml` work too.

```yaml
services:
  web:
    build: .
    expose: ["8080"]
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
    volumes:
      - ./data:/data
```

## What moin reads

- **`services`**: what runs. One of them gets the traffic: the only service, else the only one moin builds, else the one named `web`. None of these? The deploy stops and says so. Name that service `web`.
- **`build` or `image`**: what moin builds on the server, or pulls.
- **`ports` or `expose`**: the port a service listens on. Without them: its env `PORT`, then `EXPOSE` in the `Dockerfile`, then 8080.
- **`healthcheck`**: when the service is ready. The deploy waits until Docker says it is healthy. The main service must also run 10 seconds without a restart, with or without one.
- **`volumes`**: where the data is. moin keeps it outside each release, and [backs up](/docs/guides/backups) the databases it finds there.
- **`${NAME}`**: an [env var](/docs/guides/env) from moin, else from a `.env` in your repo. Docker fills it in when the containers start. The compose file moin keeps names it, never the value. [In compose.yml](/docs/guides/env#in-composeyml)

## Everything else is in moin

Domains, the instant address, the password, backups, uptime and env vars live in moin. Set them on the web, in the CLI, the TUI, the API or MCP. There is no second place, so nothing in the repo overrides them. A `compose.yml` that still has an `x-moin` block fails the deploy: remove the block and set those things in moin.

The app's name is in moin too. `moin deploy` finds it on its own: the app that follows the repo of the folder, else the folder's name.

## The compose file moin runs

moin turns your `compose.yml` into the file it runs: your services with their volumes, env files and builds, plus the proxy that serves your domains with HTTPS. It is the same file after every deploy, so you always see what runs. No secret value, no password hash. Every `${NAME}` stays as you wrote it, but in the settings the deploy filled in: `image`, `ports`, `expose`, `volumes`, `user`, the env `PORT` and the paths of a `build` show the value they ran with.

Web: the app's **Settings** tab, **Compose file**: **Copy** or **Download**

CLI: `moin compose [app] > compose.yml`

TUI: `C` on the app opens it in the web.

API: `GET /api/v1/apps/{app}/compose`. MCP: `get_compose_file`.

It is the one of the live deploy. A deploy without one, such as one from before this feature, says so: deploy again. If it stays empty, the deploy log says why.

### Run it elsewhere

The compose file is your way out. Nothing in it needs moin.

1. Save it as `compose.yml` in a folder, next to your repo's files when it builds from them.
2. Put a `.env` next to it with your env vars, and the values of the `.env` in your repo if it has one. Docker fills `${NAME}` from it, and the app reads it too.
3. Point your domains at the new server.
4. Run `docker compose up -d`.

The proxy takes ports 80 and 443 and gets the certificates from Let's Encrypt, as on your server. It does that also when your server gets its traffic through a [Cloudflare Tunnel](/docs/guides/security#web), which only moin sets up.

Each file brings its own proxy on 80 and 443. For several apps on one server, keep the proxy of one file, delete it from the others, and write `external: true` under `moin-proxy` in their `networks`.

What changed on the way out:

- **Source**: the folder of the file is your repo, `.`. A service moin built has `build: .` again. For a Go repo without a `Dockerfile`, moin writes one into the compose file, so it comes along.
- **Data**: the folders moin kept outside each release are where your `compose.yml` put them, like `./data`. Copy the data there first. Named volumes keep their names.
- **Instant address**: it stays with moin. It names your server's IP, so the file routes only your domains.
- **Password**: when the app has one, the proxy reads it from `./htpasswd`. Make it with `htpasswd -nB moin > htpasswd`, with your user if you set one.
- **Railpack apps**: moin builds them with [Railpack](https://railpack.com), whose build plan is not in your repo. The file keeps the image moin built, which is only on your server. Run `railpack build .` in your repo there and put the image's name in the file, or push the image to a registry.

## Good to know

- Only a `Dockerfile` or a Go module? You need no `compose.yml`. moin writes one service for it.
- After every deploy the daemon reports what it found: the service, its port, the databases. The app page, the end of `moin deploy` and `get_app` show it.
- Secrets never belong in the repo. Use [env vars](/docs/guides/env).
