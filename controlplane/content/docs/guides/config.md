---
title: "Config in the repo"
description: "The x-moin block in compose.yml"
order: 5
---

Everything moin needs to know about an app can live next to its code, in `compose.yml` at the top of your repo (`compose.yaml` and `docker-compose.yml` work too). Every field is optional; without them moin detects what it can, as [Deploy](/docs/guides/deploy#deploy-from-a-repo) shows.

Your repo has only a `Dockerfile` or a `go.mod`, or none of them and moin [builds it with Railpack](/docs/guides/deploy#no-dockerfile)? Then you need no file at all. To set one of these fields anyway, add a `compose.yml` like the one below: it then replaces the one moin would write, so it needs the `services` part too, with a `Dockerfile` to build. For a Railpack app, change the start command with the env var `RAILPACK_START_CMD` instead.

```yaml
x-moin:
  app: <app-name>              # default: the directory name
  server: <server-name>        # where the first deploy creates the app
  service: web                 # gets the traffic; default: the only service, the only built one, or web
  port: 8080                   # default: ports, expose, PORT, Dockerfile EXPOSE, then 8080
  health: /healthz             # called after the start, must answer below 500
  domains: [<domain>, www.<domain>]   # or by service: {web: [<domain>], api: [api.<domain>]}
  canonical: <domain>          # the others redirect here
  ports: {api: 5678}           # the port of each other service with domains
  protect: true                # ask for the password set in moin
  instant: sslip.io            # the instant address: sslip.io, nip.io or off
  database: /data/app.db       # a SQLite file on a volume, or postgres-<service>

services:
  web:
    build: .
    volumes:
      - ./data:/data
```

Docker ignores `x-` blocks, so the file still works with plain `docker compose`.

The agent reports what it detected after every deploy: kind, service, port, databases and this block. The web UI shows it on the app page, the TUI on the app, `moin deploy` at the end and MCP in `get_app`.

Fields in `x-moin` win over the settings you make in moin, such as [domains](/docs/guides/domains#in-the-repo) and the [instant address](/docs/guides/domains#instant-address). `database` picks the file the [data browser](/docs/guides/data) and the [backups](/docs/guides/backups) use when the app has several.

Secrets never belong in the repo. Use [env vars](/docs/guides/env).
