---
title: "Run a command"
description: "A one-off command in an app's container, without SSH"
order: 11
---

## Run a command

Runs one command in a container of your app and shows what it prints as it comes, with the exit code at the end. Use it for migrations, to create the first admin user or to look at a file. No SSH, no key on your laptop: it goes over the connection the agent already has.

```surfaces
Web: the app page, **Run a command**
CLI: moin exec [app] [--service web] -- <command>
TUI: on the app, `:exec <command>`
API: POST /api/v1/apps/{app}/exec
MCP: exec_command
```

The classic ones:

```bash
moin exec shop -- rails db:migrate
moin exec shop -- php artisan migrate
moin exec shop -- rails runner 'User.create!(email: "me@example.com", admin: true)'
moin exec shop -- ls /data
moin exec shop --service db -- psql -U shop -d shop -c "select count(*) from orders"
```

Everything after `--` is the command, word by word. `moin exec` exits with the command's exit code, so it fits in a script.

## How it runs

The agent runs `docker compose exec` in the live release, in the app's main service unless you pick another one with `--service`. The web page offers the main service and the database.

- **No shell.** The command runs as it is, so `|`, `>`, `&&` and `$VAR` mean nothing. For those, ask for a shell yourself: `moin exec shop -- sh -c 'ls /data | wc -l'`. The web page, the TUI and MCP take one line and split it like a shell quotes words, `'…'` and `"…"`, nothing more.
- **Not interactive.** There is no terminal and no input, so a command that asks a question waits until it times out. Pass the answer as a flag, like `--force` or `--no-interaction`.
- **Timeout.** moin waits 10 minutes, at most 1 hour with `--timeout 1h`. After that `moin exec` exits with `124`. Docker does not stop a command it started this way, so it may still run in the container: `moin restart` stops it.
- **Output.** Up to 1 MB; moin cuts the rest and says so. MCP keeps the last 64 KB.
- **The server must be online**, and the app live.

## Who may run commands

Owners and admins. A command can do anything the app can, so members cannot.

Every command lands in the [activity](/docs/guides/activity) as `app.exec`, with the service, the command and its exit code. Values of the app's env vars in the command show as `•••`. What the command printed is never stored.

## Turn it off on a server

`exec: off` in the [rules](/docs/guides/security#rules) of a server refuses every command through moin there, for everyone:

```
# /etc/moin/rules
exec: off
```

A refused command says so: `refused by the rules of this server (/etc/moin/rules): exec: off`.

An agent older than this feature answers `update it`. Update the agent with `moin servers update <server>`.
