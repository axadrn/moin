---
title: "Env vars"
description: "Encrypted settings and secrets per app"
order: 10
---

Env vars are stored encrypted with AES-256-GCM. A restart, a start or the next deploy hands them to every service ops builds: `ops restart` after `ops env set`. A rollback keeps the env vars of its release.

| Action | Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- | --- |
| List | app page, **Env vars** | `ops env` | `e` | `GET /api/v1/apps/{app}/env` | `get_env` |
| Show values | **Show values** | `ops env --show` | `e`, then `show` | `GET /api/v1/apps/{app}/env` | `get_env` |
| Set | **Set** | `ops env set KEY=value …` | `e`, then `KEY=value` | `PATCH /api/v1/apps/{app}/env` with `set` | `set_env` |
| Remove | **Remove** | `ops env rm KEY …` | `e`, then `-KEY` | `PATCH /api/v1/apps/{app}/env` with `unset` | `set_env` with `unset` |

Reading values needs an API token with write access: reading a secret is as strong as writing it.

## Use them locally

```bash
ops env pull           # writes .env with mode 0600
ops run -- go run .    # runs a command with the env vars, without a file
```

Commands take the app from `--app`, from `x-ops.app` or from the directory name.
