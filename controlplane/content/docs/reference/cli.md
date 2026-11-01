---
title: "CLI"
description: "Every ops command"
order: 1
---

| Command | Does |
| --- | --- |
| `ops` | opens the terminal UI |
| `ops login [--url <url>] [--token <token>]` | saves a personal API token; without `--token` it asks, or reads it from stdin: `echo "$TOKEN" \| ops login`. `--url` defaults to the saved control plane, else `https://ops.goilerplate.com` |
| `ops logout` | forgets it |
| `ops activity [--limit 30]` | who deployed, restored or changed what, newest first |
| `ops team` | members and open invitations of your workspace |
| `ops team invite <email> [--admin]` | invites someone by email |
| `ops servers` | lists servers |
| `ops servers add [name]` | adds a server, prints its install command; without a name it is the next free `web-1`, `web-2`, … |
| `ops servers rm <name> [--yes]` | removes a server |
| `ops servers update [name]` | updates the agent to the control plane's version; without a name every online server whose agent differs |
| `ops servers rules <name>` | the server's own rules for ops, from `/etc/ops/rules` on it |
| `ops apps` | lists apps |
| `ops apps add --server <server> <name>` | adds an app |
| `ops apps rm <app> [--yes]` | removes an app, keeps its data |
| `ops deploy [--dir .] [--server <server>] [--detach] [app]` | deploys a directory |
| `ops deploy --image <image> [app]` | deploys an image built in CI |
| `ops redeploy [app]` | runs the newest source or image again |
| `ops restart [app]` | starts the live release again with the current env and domains |
| `ops stop [app] [--yes]` | takes the app offline; its data stays |
| `ops start [app]` | runs a stopped app again |
| `ops deploys [app]` | the newest deploys: number, status, started, took, note |
| `ops deploys [app] v<number>` | one deploy with its whole log |
| `ops hook [app]` | prints a new deploy hook URL for CI |
| `ops registry` | lists registry logins |
| `ops registry set <host> <username>` | saves a login; the password or token comes from stdin |
| `ops registry rm <host> [--yes]` | removes a login |
| `ops rollback [app] [v<number>]` | starts an earlier release again; without a number the one before the live one |
| `ops logs [--tail 200] [app]` | prints container logs |
| `ops domains [app]` | lists domains and their DNS |
| `ops domains add <domain> ...` | adds domains |
| `ops domains rm <domain> ... [--yes]` | removes domains |
| `ops domains canonical <domain\|none>` | the domain the others redirect to |
| `ops protect [--user ops] [app]` | asks for a password; the password comes from stdin |
| `ops protect off [app]` | makes the app public again |
| `ops db [--db <path>] [app]` | tables, row counts and migration of the database |
| `ops db rows <table> [--limit 20] [--offset 0]` | a page of a table, newest first |
| `ops db schema [table]` | the CREATE statements |
| `ops sql [--app <app>] [--limit 100] "<sql>"` | runs SQL read-only; the SQL may come on stdin |
| `ops sql --write "<sql>"` | runs SQL that changes data, in one transaction |
| `ops backups [app]` | snapshots, point in time window and restore drills |
| `ops backup [app]` | takes a snapshot now |
| `ops restore <snapshot> [--yes]` · `ops restore --at "<time>" [--yes]` | restores a snapshot, or a point in time in UTC |
| `ops backups auto on\|off [app]` | snapshot before every change |
| `ops backups bucket` · `bucket set` · `bucket rm [--yes]` | the bucket backups go to, the same for every app; the secret key comes on stdin |
| `ops deploy --ref <ref> [app]` | deploys a branch, tag or commit of the linked GitHub repo |
| `ops github` · `ops github repos` | where the GitHub App is installed, with the install link; the repos it reads |
| `ops link owner/name [--branch main] [--auto=false] [app]` | follows a repo; every push deploys unless `--auto=false` |
| `ops unlink [app] [--yes]` | stops following it |
| `ops servers secure <server> [--tunnel on\|off] [--ssh tailscale\|open] [--firewall on\|off]` | how the server is reached; prints what the agent set up |
| `ops integrations` · `integrations cloudflare` · `integrations tailscale` · `integrations rm <name> [--yes]` | the Cloudflare API token and the Tailscale auth key; keys come on stdin |
| `ops env [--show] [app]` | lists env vars, values hidden without `--show` |
| `ops env set [app] KEY=value …` | sets env vars |
| `ops env rm KEY … [--yes]` | removes env vars |
| `ops env pull [--app <app>] [file]` | writes them to `.env` |
| `ops run [--app <app>] -- <command>` | runs a command with them |
| `ops agent register --url <url> --token <install token>` | connects a server, done by the install script |
| `ops agent` | runs the agent |
| `ops version` | prints the version |

Commands that work on an app take it as an argument or as `--app`, then from `x-ops.app` in `compose.yml`, then from the directory name. Flags may come before or after the arguments: `ops rollback v3 --app shop` is `ops rollback --app shop v3`. When there is no such app, the error names it and where the name came from. Every command shows its usage and flags with `--help`.

## Asking first

Commands that remove or replace something ask `[y/N]` on a terminal: `servers rm`, `apps rm`, `env rm`, `domains rm`, `registry rm`, `integrations rm`, `backups bucket rm`, `unlink` and `restore`. `--yes` skips the question. Without a terminal, as in CI, they refuse unless `--yes` is there, so a script never removes something by accident.

## Agent updates

`ops servers` shows each agent's version and marks one that differs from the control plane's. `ops servers update` has the agent download the `ops` binary the control plane serves, from the control plane URL it already uses, check its SHA-256, swap it in and restart through systemd. It waits until the new agent is back. The agent refuses while a deploy runs, and on a server with rules in `/etc/ops/rules`, since a new binary could allow what they stop. There, and for agents older than the update, update it on the server as root:

```bash
curl -fsSL https://ops.goilerplate.com/download/ops-linux-amd64 -o /usr/local/bin/ops.new && chmod 755 /usr/local/bin/ops.new && mv /usr/local/bin/ops.new /usr/local/bin/ops && systemctl restart ops-agent
```

Use `ops-linux-arm64` on an ARM server, and your own URL when you host the control plane. The error of a refused update prints the command for that server.

`/download/SHA256SUMS` lists the checksums of every binary.

## Output

On a terminal the CLI shows colors, status symbols (`● online`, `◐ deploying v4`, `✗ failed v4 · v3 live`) and a spinner while a deploy runs. Piped or in CI it prints plain text: tables keep their columns with the bare status words, so scripts can read them. Errors start with `✗`, name the command to run next and exit with 1.

## Environment

| Variable | Does |
| --- | --- |
| `OPS_URL`, `OPS_TOKEN` | override the saved login, for CI |
| `OPS_STATE_DIR` | where the agent keeps its state, default `/var/lib/ops` |
| `NO_COLOR` | turns colors off on a terminal too |
