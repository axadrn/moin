---
title: "Logs"
description: "The output of an app's containers"
order: 11
---

## Read the logs

Shows the last lines your app printed, from every container of the app. Use it when something goes wrong.

```surfaces
Web: the app page, **Container logs**, **Load logs**
CLI: ops logs <app-name>
TUI: on the app, `L`; `j` `k` scroll, `G` follows the end, `/` searches
API: GET /api/v1/apps/{app}/logs
MCP: get_logs
```

`ops logs --tail 500` reads more lines. The agent reads them with `docker compose logs`, and the server must be online.

Each line starts with the service and a timestamp:

```text
web-1  | 2026-10-31T09:05:32.211103972Z listening on 8080
```
