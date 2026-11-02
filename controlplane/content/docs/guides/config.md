---
title: "Config in the repo"
description: "The x-ops block in compose.yml"
order: 5
---

Everything ops needs to know about an app can live next to its code, in `compose.yml` at the top of your repo (`compose.yaml` and `docker-compose.yml` work too). Every field is optional; without them ops detects what it can.

Your repo has only a `Dockerfile` or a `go.mod`? Then you need no file at all. To set one of these fields anyway, add a `compose.yml` like the one below: it then replaces the one ops would write, so it needs the `services` part too.

```yaml
x-ops:
  app: blog              # default: the directory name
  server: web-1          # where the first deploy creates the app
  service: web           # gets the traffic; default: the only service, the only built one, or web
  port: 8080             # default: ports, expose, PORT, Dockerfile EXPOSE, then 8080
  health: /healthz       # called after the start, must answer below 500
  domains: [blog.de, www.blog.de]
  canonical: blog.de     # the others redirect here
  protect: true          # ask for the password set in ops
  database: /data/app.db # must sit on a volume, or the deploy fails

services:
  web:
    build: .
    volumes:
      - ./data:/data
```

Docker ignores `x-` blocks, so the file still works with plain `docker compose`.

The agent reports what it detected after every deploy: kind, service, port, databases and this block. The web UI shows it on the app page, the TUI on the app, `ops deploy` at the end and MCP in `get_app`.

Fields in `x-ops` win over the settings you make in ops, such as [domains](/docs/guides/domains#in-the-repo).

Secrets never belong in the repo. Use [env vars](/docs/guides/env).
