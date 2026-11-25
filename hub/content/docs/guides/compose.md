---
title: "compose.yml"
description: "What moin reads from compose.yml. It is plain Docker, and every moin setting is in moin."
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

Domains, the instant address, the password, backups, uptime and env vars live in moin. Set them on the web, in the CLI, the TUI, the API or MCP. There is no second place, so nothing in the repo overrides them.

The app's name is in moin too. `moin deploy` finds it on its own: the app that follows the repo of the folder, else the folder's name.

## Good to know

- Only a `Dockerfile` or a Go module? You need no `compose.yml`. moin writes one service for it.
- After every deploy the daemon reports what it found: the service, its port, the databases. The app page, the end of `moin deploy` and `get_app` show it.
- Secrets never belong in the repo. Use [env vars](/docs/guides/env).
