---
title: "Run a command"
description: "A one-off command in an app's container. No SSH."
group: "Every day"
order: 3
---

Run one command inside your app and see what it prints. Good for migrations, the first admin user, or a look at a file. No SSH. No key on your laptop.

```text
you: moin exec my-app -- ls /data
  │                       ▲
  ▼                       │
hub                       │
  │                       │
  ▼                       │
daemon, on its open line  │
  │                       │
  ▼                       │
container of my-app ──────┘
runs ls /data, prints the output
```

## Run a command

```surfaces
Web: the app's **Logs** tab, **Run a command**
CLI: moin exec [app] [--service web] -- <command>
TUI: on the app, `X`, then type the command
API: POST /api/v1/apps/{app}/exec
MCP: exec_command
```

```bash
moin exec my-app -- rails db:migrate
moin exec my-app -- php artisan migrate
moin exec my-app -- ls /data
moin exec my-app --service db -- psql -U app -d app -c "select count(*) from users"
```

Everything after `--` is the command, word by word. For pipes, ask for a shell: `moin exec my-app -- sh -c 'ls /data | wc -l'`. It runs in the main service unless `--service` names another.

## Good to know

| | |
| --- | --- |
| No shell | pipes, `>`, `&&` and `$VAR` mean nothing; ask for a shell, as above |
| No input | a command that asks waits until it times out. Pass `--force` or the like |
| Timeout | 10 minutes, at most 1 hour with `--timeout 1h`; then `moin exec` exits with `124` |
| Output | up to 1 MB, the rest is cut and moin says so. MCP keeps the last 64 KB |
| Needs | the server online and the app live |
| Who | owners and admins only: a command can do anything the app can |
| Activity | every command lands in the [activity](/docs/guides/activity) as `app.exec`; the output is never stored |

## Turn it off on a server

`exec: off` in the [rules](/docs/guides/security#rules) of a server refuses every command through moin there:

```
# /etc/moin/rules
exec: off
```
