---
title: "Config in the repo"
description: "The x-moin block in compose.yml"
group: "Apps"
order: 7
---

Your app's settings can live in your repo, next to the code. Put an `x-moin` block in `compose.yml`, at the top of your repo. Every field is optional.

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
  backups:
    every: 1h                  # how often: 15m, 1h or 1d
    keep: 7d                   # how long: 1d, 3d, 7d or 30d

services:
  web:
    build: .
    volumes:
      - ./data:/data
```

`x-moin` is optional. Docker ignores `x-` blocks, so the file still works with plain `docker compose`. `compose.yaml` and `docker-compose.yml` work too.

## Who decides

If a setting is in `x-moin`, the file decides. If it is not, moin decides. There is no switch for this.

A setting in the file is locked in moin, marked **from compose.yml**. To change it, change the file and deploy.

## Good to know

- After you [rename the app](/docs/guides/names#rename-an-app), set `app` to its new name. Otherwise `moin deploy` in a folder of the old name offers to make a new app.
- `backups` counts as a whole. A part you leave out takes its default: hourly, 7 days. A wrong value stops the deploy and says why.
- Only a `Dockerfile` or a Go module? You need no file. To set a field anyway, add a `compose.yml` with a `services` part that builds a `Dockerfile`.
- After every deploy the daemon reports what it found. The app page, the end of `moin deploy` and `get_app` show it.
- Secrets never belong in the repo. Use [env vars](/docs/guides/env).
