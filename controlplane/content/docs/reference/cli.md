---
title: "CLI"
description: "Every ops command"
order: 1
---

| Command | Does |
| --- | --- |
| `ops` | opens the terminal UI |
| `ops login [--url] [--token]` | saves a personal API token |
| `ops logout` | forgets it |
| `ops activity [--limit 30]` | who deployed, restored or changed what, newest first |
| `ops team` | members and open invitations of your workspace |
| `ops team invite <email> [--admin]` | invites someone by email |
| `ops servers` | lists servers |
| `ops servers add <name>` | adds a server, prints its install command |
| `ops servers rm <name>` | removes a server |
| `ops servers rules <name>` | the server's own rules for ops, from `/etc/ops/rules` on it |
| `ops apps` | lists apps |
| `ops apps add --server <server> <name>` | adds an app |
| `ops apps rm <name>` | removes an app, keeps its data |
| `ops deploy [--dir .] [--server <server>] [--detach] [app]` | deploys a directory |
| `ops deploy --image <image> [app]` | deploys an image built in CI |
| `ops redeploy [app]` | runs the newest source or image again |
| `ops hook [app]` | prints a new deploy hook URL for CI |
| `ops registry` | lists registry logins |
| `ops registry set <host> <username>` | saves a login; the password or token comes from stdin |
| `ops registry rm <host>` | removes a login |
| `ops rollback [--app <app>] [v<number>]` | starts an earlier release again |
| `ops logs [--tail 200] [app]` | prints container logs |
| `ops domains [app]` | lists domains and their DNS |
| `ops domains add <domain> ...` | adds domains |
| `ops domains rm <domain> ...` | removes domains |
| `ops domains canonical <domain\|none>` | the domain the others redirect to |
| `ops protect [--user ops]` | asks for a password; the password comes from stdin |
| `ops protect off` | makes the app public again |
| `ops apply [app]` | restarts the running release with the current env and domains |
| `ops db [--app <app>] [--db <path>]` | tables, row counts and migration of the database |
| `ops db rows <table> [--limit 20] [--offset 0]` | a page of a table, newest first |
| `ops db schema [table]` | the CREATE statements |
| `ops sql [--app <app>] [--limit 100] "<sql>"` | runs SQL read-only; the SQL may come on stdin |
| `ops sql --write "<sql>"` | runs SQL that changes data, in one transaction |
| `ops backups [--app <app>]` | snapshots, point in time window and restore drills |
| `ops backup [app]` | takes a snapshot now |
| `ops restore <snapshot>` · `ops restore --at "<time>"` | restores a snapshot, or a point in time in UTC |
| `ops backups auto on\|off` | snapshot before every change |
| `ops backups bucket` · `bucket set` · `bucket rm` | the bucket backups go to; the secret key comes on stdin |
| `ops deploy --ref <ref> [app]` | deploys a branch, tag or commit of the linked GitHub repo |
| `ops github` · `ops github repos` | where the GitHub App is installed, with the install link; the repos it reads |
| `ops link owner/name [--branch main] [--auto=false]` | follows a repo; every push deploys unless `--auto=false` |
| `ops unlink` | stops following it |
| `ops servers secure <server> [--tunnel on\|off] [--ssh tailscale\|open] [--firewall on\|off]` | how the server is reached; prints what the agent set up |
| `ops integrations` · `integrations cloudflare` · `integrations tailscale` · `integrations rm <name>` | the Cloudflare API token and the Tailscale auth key; keys come on stdin |
| `ops env [--app <app>] [--show]` | lists env vars |
| `ops env set [--app <app>] KEY=value …` | sets env vars |
| `ops env rm [--app <app>] KEY …` | removes env vars |
| `ops env pull [--app <app>] [file]` | writes them to `.env` |
| `ops run [--app <app>] -- <command>` | runs a command with them |
| `ops agent register --url <url> --token <install token>` | connects a server, done by the install script |
| `ops agent` | runs the agent |
| `ops version` | prints the version |

Commands that work on an app take it from the argument or `--app`, then from `x-ops.app` in `compose.yml`, then from the directory name.

## Output

On a terminal the CLI shows colors, status symbols (`● online`, `◐ deploying v4`, `✗ failed v4 · v3 live`) and a spinner while a deploy runs. Piped or in CI it prints plain text: tables keep their columns with the bare status words, so scripts can read them. Errors start with `✗`, name the command to run next and exit with 1.

## Environment

| Variable | Does |
| --- | --- |
| `OPS_URL`, `OPS_TOKEN` | override the saved login, for CI |
| `OPS_STATE_DIR` | where the agent keeps its state, default `/var/lib/ops` |
| `NO_COLOR` | turns colors off on a terminal too |
