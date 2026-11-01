---
title: "Config in the repo"
description: "The x-ops block in compose.yml"
order: 5
---

Everything ops needs to know about an app can live next to its code, in `compose.yml`. Every field is optional; without them ops detects what it can.

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

Secrets never belong in the repo. Use [env vars](/docs/guides/env).
