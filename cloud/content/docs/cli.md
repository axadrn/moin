---
title: "CLI"
description: "Every moin command, in one pattern, and the terminal UI. Name the app, and moin finds its server. --json on every reading command, for scripts and AI agents."
group: "Reference"
order: 1
---

`moin` is one program. On your laptop it is the CLI and the terminal UI. On a server it does the work. `moin help` lists every command, and every command shows its flags with `--help`.

## One pattern

1. **Words without dashes are the command.** A list is a plural noun: `moin domains`. A change is a noun and a verb: `moin domains add`. An action is a verb: `moin deploy`.
2. **Then what it acts on**: the app, the server, the domain. `moin domains add shop shop.example.com`.
3. **Everything with `--` is optional.** Leave it out and the sensible thing happens. Only three have a short form: `-s` (`--server`), `-f` (`--follow`), `-y` (`--yes`).

Flags may come before or after the words: `moin logs shop -f` is `moin logs -f shop`.

## Name the app, from any folder

You name the app: `moin logs shop`. moin finds the server it runs on. In the app's folder you may leave the name out: `moin logs`.

- The same app on two servers, like staging and prod? moin asks for `-s prod`. It never guesses.
- A server is a name from your SSH config, like `prod`, or `user@host`. If `ssh prod` works, moin works.
- moin keeps your servers in `~/.config/moin/servers`, one per line, and which app runs where in `~/.config/moin/apps.json`. On a miss, it asks every server.

When something is missing and you are on a terminal, moin asks: a list to pick from, or a question. In a script it stops and names the flag to pass.

## The terminal UI

`moin` alone, on a terminal, opens the terminal UI. Every server and every app, live. Every server is asked again every 5 seconds.

```bash
moin
```

Pick an app and press Enter: its backups, and its logs as they come.

| Key | What it does |
| --- | --- |
| `↑` `↓`, or `k` `j` | select an app |
| `enter` | its logs, following, and its backups |
| `esc` | back to every app |
| `r` | restart the app, after you say `y` |
| `b` | go back to an older release, after you say `y` |
| `o` | open the app's address in your browser |
| `?` | the keys |
| `q` | quit |

The terminal UI is only a view on the same commands. Deploy from the app's folder with `moin deploy`. Everything else is a command below.

Without servers, or without a terminal, `moin` prints the table instead. So does `moin apps`.

## Status

| Command | What it does |
| --- | --- |
| `moin` | the terminal UI, or a table of every server and app, and what needs you |
| `moin apps` | the table: every app on every server, its release, its state and its address |
| `moin servers` | your servers, the moin version on each, and the watchers each one reports to |
| `moin servers add <server>` | add a server you deployed to before, like on a new laptop. A first deploy adds it too |
| `moin servers rm <server>` | take a server off your list. Nothing changes on it |

Under the table, `moin` says what needs you: a server no watcher watches, an app that does not run.

## Deploy

| Command | What it does |
| --- | --- |
| `moin deploy prod` | the first deploy of the app in this folder, to `prod`. Sets up moin there first |
| `moin deploy` | deploy again, to the server the app runs on |
| `moin deploy prod --image shop:dev` | send an image from your laptop instead of the source |
| `moin deploy prod --catalog umami` | an app from the [catalog](/docs/catalog) |
| `moin deploy --app shop` | another name for the app. Without it, the app is named after the folder |
| `moin rollback [app]` | pick an older release to run again. `--to v3` names it. Without a terminal, the one before |
| `moin restart [app]` | start the live release again, with its env and domains as they are now |
| `moin stop [app]` | stop the app. It stays stopped after a reboot, until `moin start`. Its data stays |
| `moin start [app]` | start a stopped app |
| `moin logs [app]` | the last 200 lines. `-f` follows them, `--tail 1000` shows more |
| `moin exec [app] -- <command>` | run one command in the app's container. `--service db` picks another service |
| `moin catalog` | the apps of the catalog |

The first deploy to a server asks first: `Set up moin on prod and deploy shop? [y/N]`. In a script, add `-y`. [Deploy](/docs/deploy)

## Env vars and domains

| Command | What it does |
| --- | --- |
| `moin env [app]` | the names of the env vars, never the values |
| `moin env edit [app]` | open `$EDITOR` with the values. The app restarts with them |
| `moin domains [app]` | the app's domains |
| `moin domains add [app] <domain>` | add a domain, with HTTPS |
| `moin domains rm [app] <domain>` | remove one. It asks first |

## Backups

| Command | What it does |
| --- | --- |
| `moin backups [app]` | the copies, the last restore check, and which copy it proved |
| `moin backup [app]` | a copy now |
| `moin restore [app]` | pick a copy to go back to, or the undo of the last restore |
| `moin restore [app] --to <copy>` | the same, in a script: a copy from `moin backups`, a time like `2026-11-27T14:07` in UTC, or `undo-…` |
| `moin bucket <server>` | the bucket of the server, never its secret |
| `moin bucket set <server>` | set the bucket. moin asks for each value |

An app with several databases: `moin restore` asks which one, `--database <name>` names it.

In a script, `moin bucket set` takes `--endpoint`, `--bucket` and `--access-key`, and reads the secret key from stdin. `--region` and `--path` are optional. [Backups](/docs/backups#the-bucket)

## The team and alerts

| Command | What it does |
| --- | --- |
| `moin keys <server>` | the SSH keys that may log in as root: your team |
| `moin keys add <server> <file.pub>` | let one more key in |
| `moin keys rm <server> <name>` | take a key out, by its name or its fingerprint |
| `moin alerts <server>` | where the server sends its alerts, numbered |
| `moin alerts add <server> telegram --token … --chat-id …` | alerts to Telegram |
| `moin alerts add <server> slack --url …` | alerts to Slack. `discord` and `webhook` take `--url` too |
| `moin alerts rm <server> <n>` | remove one, by its number in `moin alerts` |
| `moin alerts test <server>` | send a test alert to each |

`moin alerts add` sends a test alert first, and keeps it only when that went out. [Security](/docs/security#the-team)

## Watcher

| Command | What it does |
| --- | --- |
| `moin watcher run` | run a watcher, on another machine than your servers |
| `moin watcher token <server>` | on the watcher: a token for a server, and the exact line to paste on your laptop |
| `moin watcher connect <server> <url> <token>` | on your laptop: the server reports to that watcher |
| `moin watcher disconnect <server> <url>` | the server stops reporting there |

[Watcher](/docs/watcher)

## Updates and the server

| Command | What it does |
| --- | --- |
| `moin update` | update the CLI on your laptop to the newest release |
| `moin update <server>` | put the version of your CLI on the server |
| `moin daemon uninstall` | on the server: remove moin. The apps keep running |
| `moin daemon uninstall --apps` | on the server: remove moin and every app, with its data |
| `moin version` | the version |
| `moin help` | every command |

Coming: `moin secure <server>` and `moin unlock`. [Security](/docs/security)

## Remove moin from a server

Log in to the server, and run it there as root:

```bash
ssh -t prod
moin daemon uninstall
```

```text
? Remove moin from this server? The apps keep running. [y/N] y
→ stopping the moin-daemon service
→ removing /usr/local/bin/moin
✓ moin is removed from this server
  Still here: the apps shop with their data in /var/lib/moin/apps; the proxy container moin-traefik, and the tunnel and firewall when they were on; the config in /etc/moin/config.yml; Docker.
  Remove the apps and their data too, as root, with moin daemon uninstall --apps
```

- Without `--apps`: the daemon goes, and with it the backups, the restore checks and the reports. The apps keep running, with their data and HTTPS.
- With `--apps`: every app goes too, with its data, its images, the proxy, the tunnel and the firewall. This can't be undone.
- What stays either way: the config in `/etc/moin/config.yml`, and Docker.
- In a script, add `-y`.

Then, on your laptop: `moin watcher disconnect prod <url>` for each watcher, or it says the server went silent. And `moin servers rm prod`.

## Asking first

A command that deletes or replaces something asks first, like a restore, `moin keys rm` or `moin domains rm`. On a terminal you answer `y`. In a script it needs `-y`, or it stops.

## For scripts: --json

Every command that reads takes `--json`, and prints JSON instead of a table:

```bash
moin --json
moin backups shop --json
```

`moin deploy`, `moin rollback`, `moin backup` and `moin restore` take it too. Then the log goes to stderr, and stdout holds only the JSON.

A command that fails exits with a code other than 0, and says why. `moin exec` exits with the code of the command it ran.

## New versions

Once a day, the CLI asks GitHub for the newest release. When one is out, it says so above the command: `moin v1.2.4 is out, you run v1.2.3.` It never updates by itself. [Updates](/docs/security#updates)

## AI agents

There is no MCP server, and none is needed. AI coding agents like Claude Code, Codex and Cursor use the CLI like you do.

- They read with `--json`.
- Anything that deletes needs `-y`, so it shows in the agent's transcript.
- They run with your SSH key, so they can do what you can. Give an agent what you would give a colleague.

Put this in your project's `AGENTS.md` or `CLAUDE.md`:

```markdown
Deploy with `moin deploy prod`. Check with `moin --json`.
```
