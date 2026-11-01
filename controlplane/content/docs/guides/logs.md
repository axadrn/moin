---
title: "Logs"
description: "The output of an app's containers"
order: 11
---

The agent reads the last lines of every container of an app with `docker compose logs`.

| Web | CLI | TUI | MCP |
| --- | --- | --- | --- |
| app page, **Load logs** | `ops logs [--tail 200] [app]` | `l` | `get_logs` |

Each line starts with the service and a timestamp:

```text
web-1  | 2026-10-31T09:05:32.211103972Z listening on 8080
```

The server must be online to read logs.
