---
title: "Config in the repo"
description: "The x-moin block in compose.yml"
group: "Apps"
order: 7
---

Everything moin needs to know about an app can live next to its code, in an `x-moin` block in `compose.yml` at the top of your repo (`compose.yaml` and `docker-compose.yml` work too). Every field is optional.

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

## Good to know

- Fields in `x-moin` win over the settings in moin, such as [domains](/docs/guides/domains#in-the-repo) and the [instant address](/docs/guides/domains#instant-address).
- `database` picks the database the [data browser](/docs/guides/data) and the [backups](/docs/guides/backups) use when the app has several.
- Docker ignores `x-` blocks, so the file still works with plain `docker compose`.
- Only a `Dockerfile` or `go.mod`, or neither? You need no file. To set a field anyway, add a `compose.yml` with a `services` part that builds a `Dockerfile`: it replaces the one moin would write. For a Railpack app, set the start command with the env var `RAILPACK_START_CMD` instead.
- After every deploy the agent reports what it detected: kind, service, port, databases and this block. The app page, the end of `moin deploy` and `get_app` show it.
- Secrets never belong in the repo: use [env vars](/docs/guides/env).
