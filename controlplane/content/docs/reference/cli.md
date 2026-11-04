---
title: "CLI"
description: "Every moin command"
order: 1
---

| Command | Does |
| --- | --- |
| `moin` | opens the terminal UI |
| `moin login [--url <url>] [--token <token>]` | saves a personal API token; without `--token` it asks, or reads it from stdin: `echo "$TOKEN" \| moin login`. `--url` defaults to the saved control plane, else `https://moin.run`. It must be https; plain http works only for localhost and your tailnet (`100.x` addresses, `*.ts.net`) |
| `moin logout` | forgets it |
| `moin alerts [on\|off]` | shows or sets whether you get a mail when a site goes down or comes back, a server goes offline or comes back, or a deploy fails, in the workspaces you own or run as admin; on by default. See [Alerts](/docs/guides/alerts) |
| `moin alerts add slack\|discord\|telegram\|webhook <name> [--chat <id>] [--events <events>]` | sends alerts to Slack, Discord, Telegram or a webhook too; the URL, or the bot token for Telegram, comes on stdin, `--chat` is the Telegram chat, `--events` a comma list like `uptime.down,deploy.failed` (default all). A webhook prints its signing secret once. `moin alerts` lists the destinations. Owners and admins. See [Alerts](/docs/guides/alerts#slack-discord-telegram-and-webhooks) |
| `moin alerts test <name>` | sends a test alert to a destination |
| `moin alerts rm <name> [--yes]` | removes a destination |
| `moin activity [--app <app>] [--server <server>] [--user <email>] [--limit 30] [--cursor <cursor>]` | who deployed, restored or changed what, newest first, to the second in your time zone; the hint at the end pages back |
| `moin team` | members and open invitations of your workspace |
| `moin team invite <email> [--admin]` | invites someone by email |
| `moin team resend <email>` | sends an invitation again; only the new link works |
| `moin team revoke <email> [--yes]` | takes an invitation back |
| `moin team rename <name>` | renames the workspace; its links stay the same |
| `moin status` | is everything OK: what needs you, catalog updates and what runs; exits `1` when something needs you ([Overview](/docs/guides/overview)) |
| `moin servers` | lists servers |
| `moin servers add [name]` | adds a server, prints its install command; without a name it gets one like `brave-otter` |
| `moin servers install-command <name>` | prints a new one-time install command; the old one stops working |
| `moin servers rename <name> <new name>` | renames a server; its apps stay on it |
| `moin servers move <name> up\|down\|top\|bottom\|<position>` | moves a server in the order every list shows; a position counts from 1 |
| `moin servers rm <name> [--yes]` | asks you to type its name, removes the server and prints the command that removes its agent there |
| `moin servers update [name]` | updates the agent to the control plane's version now; without a name every online server whose agent differs |
| `moin servers auto-update [on\|off]` | shows or sets whether agents update to the control plane's version by themselves; on by default |
| `moin servers rules <name>` | the server's own rules for moin, from `/etc/moin/rules` on it |
| `moin apps` | lists apps |
| `moin apps <app>` | one app: its state, server, domains, password, repo and deploys; `moin app <app>` works too |
| `moin apps add --server <server> <name>` | adds an app |
| `moin apps rm <app> [--yes]` | asks you to type its name, removes the app, keeps its data |
| `moin apps move <app> up\|down\|top\|bottom\|<position>` | moves an app in the order every list and the sidebar show; a position counts from 1 |
| `moin deploy [--dir .] [--server <server>] [--detach] [app]` | deploys a directory |
| `moin deploy --image <image> [app]` | deploys an image built in CI, with the compose file of an earlier deploy of the repo |
| `moin redeploy [app]` | runs the newest source or image again |
| `moin restart [app]` | starts the live release again with the current env and domains |
| `moin stop [app] [--yes]` | takes the app offline; its data stays |
| `moin start [app]` | runs a stopped app again |
| `moin deploys [app]` | the newest deploys: number, status, started, took, note |
| `moin deploys [app] v<number>` | one deploy with its whole log |
| `moin hook [app] [--yes]` | asks, then prints a new deploy hook URL for CI; the old one stops working |
| `moin hook off [app] [--yes]` | turns the deploy hook off; CI can no longer deploy |
| `moin registry` | lists registry logins |
| `moin registry set <host> <username>` | saves a login; the password or token comes from stdin |
| `moin registry rm <host> [--yes]` | removes a login |
| `moin rollback [app] [v<number>] [--yes]` | starts an earlier release again; without a number the one before the live one |
| `moin logs [--tail 200] [app]` | prints container logs |
| `moin exec [app] [--service web] [--timeout 10m] -- <command>` | [runs a command](/docs/guides/exec) in a container, no shell; exits with its code, `124` after the timeout (owner or admin) |
| `moin domains [app]` | lists domains and their DNS |
| `moin domains add [--service <service>] <domain> ...` | adds domains, to another service of compose.yml with `--service` |
| `moin domains rm [--service <service>] <domain> ... [--yes]` | removes domains |
| `moin domains canonical <domain\|none>` | the domain the others redirect to |
| `moin instant [--show] [app]` | the instant address, with the password moin made for it with `--show` |
| `moin instant sslip.io\|nip.io\|off [app]` | picks the provider of the instant address, or turns it off |
| `moin instant open\|protect [app]` | takes the password away from the instant address, or puts it back |
| `moin protect [--user moin] [app]` | asks for a password; the password comes from stdin |
| `moin protect off [app] [--yes]` | makes the app public again |
| `moin uptime [app]` | uptime of every domain over 24 hours, 7 and 30 days, the newest check and the last outages |
| `moin uptime path <domain> <path\|default>` | where the check of a domain goes; `default` is the health path or `/` |
| `moin uptime pause <domain>` · `moin uptime resume <domain>` | pauses the check of a domain, checks it every minute again |
| `moin metrics [app\|server] [--range 1h\|24h\|7d\|30d]` | CPU, memory and network of an app now, with a sparkline of the range and the deploys that went live; a name that is no app is a server |
| `moin metrics --server <server>` | CPU, memory, disk, network and load of a server, and its apps by memory |
| `moin db [--db <path>] [app]` | tables, row counts and migration of the database |
| `moin db rows <table> [--limit 20] [--offset 0]` | a page of a table, newest first |
| `moin db schema [table]` | the CREATE statements |
| `moin sql [--app <app>] [--db <path>] [--limit 100] "<sql>"` | runs SQL read-only; the SQL may come on stdin |
| `moin sql --write "<sql>" [--yes]` | runs SQL that changes data, in one transaction; DROP and DELETE ask first |
| `moin backups [app]` | snapshots, point in time window and restore drills |
| `moin backup [app]` | takes a snapshot now |
| `moin restore <snapshot> [--yes]` · `moin restore --at "<time>" [--yes]` | restores a snapshot, or a point in time in UTC: `"2026-10-31 14:01"`, or `14:01` for today; `--db <path>` picks one of several databases, `--bucket <bucket>` restores a snapshot from an older bucket the app left |
| `moin backups auto on\|off [app]` | snapshot before every change |
| `moin buckets` | your buckets, the default first, with the apps that use each and the apps that keep older backups there |
| `moin buckets add <name> --endpoint … --bucket … --access-key … [--region auto] [--path <folder>] [--default]` | adds a bucket; the secret key comes on stdin; the first one is the default |
| `moin buckets edit <name> [--name <new>] [--endpoint …] [--bucket …] [--path …] [--region …] [--access-key … \| --secret]` | changes a bucket; a new access key, or `--secret`, reads the secret key on stdin, otherwise the stored one stays |
| `moin buckets default <name>` | makes a bucket the default for apps without one of their own |
| `moin buckets rm <name> [--yes]` | removes a bucket from moin, refused while apps use it or it keeps older backups; what is in it stays |
| `moin buckets use <bucket> [app]` · `moin buckets use --default [app]` | moves an app's uploads and new backups to a bucket, or back to the default; older backups stay where they are |
| `moin backups bucket` · `bucket set` · `bucket rm [--yes]` | the default bucket, as before there were several |
| `moin deploy --ref <ref> [app]` | deploys a branch, tag or commit of the linked GitHub repo |
| `moin catalog [app]` | lists the [catalog](/docs/guides/catalog) with each app's newest release, or one app with every release |
| `moin catalog install <app> --server <server> [--version <tag>] [--name <name>] [--domain <domain>]` | installs a catalog app and follows its first deploy |
| `moin update <app> [--version <tag>]` | sets a catalog app to a release, the newest by default, and deploys it after a snapshot |
| `moin github` · `moin github repos` | where the GitHub App is installed, with the install link; the repos it reads and where to give it more |
| `moin github branches <owner/name>` | the branches of a repo, the default first |
| `moin link owner/name [--branch main] [--auto=false] [app]` | follows a repo; every push deploys unless `--auto=false` |
| `moin unlink [app] [--yes]` | stops following it |
| `moin servers secure <server> [--tunnel on\|off] [--ssh tailscale\|open] [--firewall on\|off] [--yes]` | how the server is reached; prints what the agent set up |
| `moin integrations` · `integrations cloudflare` · `integrations tailscale` · `integrations rm <name> [--yes]` | the Cloudflare API token and the Tailscale auth key; keys come on stdin |
| `moin env [--show] [app]` | lists env vars, values hidden without `--show` |
| `moin env set [app] KEY=value …` | sets env vars |
| `moin env rm KEY … [--yes]` | removes env vars |
| `moin env pull [--app <app>] [--yes] [file]` | writes them to `.env`; asks before it replaces a file |
| `moin env push [--app <app>] [--yes] [file]` | replaces all env vars with a `.env` (default `.env`); shows the keys that change and asks first |
| `moin run [--app <app>] -- <command>` | runs a command with them |
| `moin agent register --url <url> --token <install token>` | connects a server, done by the install script |
| `moin agent` | runs the agent |
| `moin agent uninstall [--apps] [--yes]` | removes the agent from this server; the apps keep running unless `--apps` |
| `moin version` | prints the version of `moin` and of the control plane, and says when a newer release is out |
| `moin update [--yes]` | updates a self-hosted control plane to the newest release, for its admins, and waits until it is back |

Commands that work on an app take it as an argument or as `--app`, then from `x-moin.app` in `compose.yml`, then from the directory name. Flags may come before or after the arguments: `moin rollback v3 --app <app-name>` is `moin rollback --app <app-name> v3`. When there is no such app, the error names it and where the name came from. Every command shows its usage and flags with `--help`.

## Asking first

moin asks before it removes or replaces something. How it asks depends on how big the change is:

| | Commands | Asks |
| --- | --- | --- |
| Big, cannot be undone | `servers rm`, `apps rm`, `buckets rm`, `backups bucket rm`, `restore` | type the name: `Type <app-name> to confirm:` |
| Small or reversible | `update`, `stop`, `rollback`, `env rm`, `domains rm`, `protect off`, `registry rm`, `integrations rm`, `unlink`, `hook`, `hook off`, `team revoke`, `servers secure`, `agent uninstall`, `env pull` over an existing file, `env push`, `sql --write` with DROP or DELETE | `[y/N]` |
| Harmless | `deploy`, `redeploy`, `restart`, `start`, `backup`, adding and setting things | nothing |

`--yes` skips the question, the typed name too. Without a terminal, as in CI, they refuse unless `--yes` is there, so a script never removes something by accident. The web, the TUI and agents ask the same way; the API never asks.

## Agent updates

Agents update themselves to the control plane's version, unless `moin servers auto-update off`. `moin servers` shows each agent's version and marks one that differs from the control plane's. `moin servers update` has the agent download the `moin` binary the control plane serves, from the control plane URL it already uses, check its SHA-256, swap it in and restart through systemd. It waits until the new agent is back. The agent refuses while a deploy runs, and on a server with rules in `/etc/moin/rules`, since a new binary could allow what they stop. There, and for agents older than the update, update it on the server as root:

```bash
curl -fsSL https://moin.example.com/download/ops-linux-amd64 -o /usr/local/bin/moin.new && chmod 755 /usr/local/bin/moin.new && mv /usr/local/bin/moin.new /usr/local/bin/moin && systemctl restart moin-agent
```

Use `ops-linux-arm64` on an ARM server. The error of a refused update prints the command for that server.

`/download/SHA256SUMS` lists the checksums of every binary.

## Output

On a terminal the CLI shows colors, status symbols (`● online`, `◐ deploying v4`, `✗ failed v4 · v3 live`) and a spinner while a deploy runs. Piped or in CI it prints plain text: tables keep their columns with the bare status words, so scripts can read them. Errors start with `✗`, name the command to run next and exit with 1.

## Environment

| Variable | Does |
| --- | --- |
| `OPS_URL`, `OPS_TOKEN` | override the saved login, for CI |
| `OPS_STATE_DIR` | where the agent keeps its state, default `/var/lib/moin` |
| `NO_COLOR` | turns colors off on a terminal too |
