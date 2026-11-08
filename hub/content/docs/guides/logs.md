---
title: "Logs"
description: "The output of an app's containers"
group: "Every day"
order: 2
---

Logs are what your app writes down while it runs.

## Read the logs

```surfaces
Web: the app's **Logs** tab, the last 200 lines; **Reload** reads them again
CLI: moin logs <app-name>
TUI: on the app, `l`, live: `↑` `↓` scroll, `G` follows the end, `/` filters
API: GET /api/v1/apps/{app}/logs
MCP: get_logs
```

```text
web-1  | 2026-10-31T09:05:32Z ready
```

Each line starts with the service and a time. `moin logs --tail 500` reads more lines.

## Good to know

- The server must be online.
- `logs: off` in the [rules](/docs/guides/security#rules) of a server refuses reading logs through moin there.
