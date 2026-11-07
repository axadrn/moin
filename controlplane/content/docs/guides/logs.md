---
title: "Logs"
description: "The output of an app's containers"
group: "Every day"
order: 2
---

The last lines your app printed, from every container of the app.

## Read the logs

```surfaces
Web: the app, **Logs**, **Load logs**
CLI: moin logs <app-name>
TUI: on the app, `l`, live: `↑` `↓` scroll, `G` follows the end, `/` filters
API: GET /api/v1/apps/{app}/logs
MCP: get_logs
```

```text
web-1  | 2026-10-31T09:05:32.211103972Z listening on 8080
```

Each line starts with the service and a timestamp. `moin logs --tail 500` reads more lines.

## Good to know

- The agent reads them with `docker compose logs`, so the server must be online.
- `logs: off` in the [rules](/docs/guides/security#rules) of a server refuses reading logs through moin there.
