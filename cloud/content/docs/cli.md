---
title: "CLI"
description: "Every moin command, in one pattern. Name the app, and moin finds its server. --json on every reading command, for scripts and AI agents."
group: "Reference"
order: 1
---

`moin` is one program. On your laptop it is the CLI. On a server it does the work. A terminal UI is coming.

## One pattern

1. **Words without dashes are the command.** A list is a plural noun: `moin domains`. A change is a noun and a verb: `moin domains add`. An action is a verb: `moin deploy`.
2. **Then what it acts on**: the app, the server, the domain. `moin domains add shop shop.example.com`.
3. **Everything with `--` is optional.** Leave it out and the sensible thing happens. Only three have a short form: `-s` (`--server`), `-f` (`--follow`), `-y` (`--yes`).

## Name the app, from any folder

You name the app: `moin logs shop`. moin finds the server it runs on. In the app's folder you may leave the name out: `moin logs`.

- The same app on two servers, like staging and prod? moin asks for `-s prod`. It never guesses.
- A server is a name from your SSH config, or `user@host`.
- moin keeps your servers in `~/.config/moin/servers`, and which app runs where in `~/.config/moin/apps.json`. On a miss, it asks every server.

When something is missing and you are on a terminal, moin asks: a list to pick from, or a question. In a script it stops and names the flag to pass.

## Status

| Command | What it does |
| --- | --- |
| `moin` | every server and every app, and what needs you |
| `moin servers` | your servers, and the watchers each one reports to |
| `moin servers add <server>` | add a server by hand, like on a new laptop. A first deploy adds it too |
| `moin servers rm <server>` | take a server off your list |
| `moin apps` | every app on every server, with its live deploy |

## Deploy

| Command | What it does |
| --- | --- |
| `moin deploy prod` | the first deploy of the app in this folder, to `prod` |
| `moin deploy` | deploy again, to the server the app runs on |
| `moin deploy prod --image shop:dev` | send an image from your laptop instead of the source |
| `moin deploy prod --catalog umami` | an app from the [catalog](/docs/catalog) |
| `moin rollback [app]` | pick an earlier deploy to run again. In a script: `--to v3` |
| `moin restart [app]` | start the live deploy again, with the newest env vars |
| `moin stop [app]`, `moin start [app]` | take the app offline, and back. Its data stays |
| `moin logs [app]` | the last 200 lines. `-f` follows them, `--tail 1000` shows more |
| `moin exec [app] -- <command>` | run one command in the app's container |
| `moin catalog` | the apps of the catalog |

`--app <name>` names the app of a deploy. Without it, the app is named after the folder. [Deploy](/docs/deploy)

## Env vars and domains

| Command | What it does |
| --- | --- |
| `moin env [app]` | the names of the env vars, never the values |
| `moin env edit [app]` | open `$EDITOR` with the values |
| `moin domains [app]` | the domains, and if each points at the server |
| `moin domains add [app] <domain>` | add a domain, with HTTPS |
| `moin domains rm [app] <domain>` | remove one |

## Backups

| Command | What it does |
| --- | --- |
| `moin backups [app]` | the copies, the last restore check, and which copy it proved |
| `moin backup [app]` | a copy now |
| `moin restore [app]` | pick a copy to go back to, or the undo of the last restore |
| `moin restore [app] --to <copy>` | the same, in a script: a copy from `moin backups`, a time like `2026-11-27T14:07` in UTC, or `undo-…` |
| `moin bucket <server>` | the bucket of the server, never its secret |
| `moin bucket set <server>` | set the bucket. moin asks for each value |

In a script, `moin bucket set` takes `--endpoint`, `--bucket` and `--access-key`, and reads the secret from stdin. [Backups](/docs/backups)

## The team and alerts

| Command | What it does |
| --- | --- |
| `moin keys <server>` | the SSH keys of the server: your team |
| `moin keys add <server> <file.pub>` | let someone in |
| `moin keys rm <server> <name>` | take them out |
| `moin alerts <server>` | where the server sends its alerts |
| `moin alerts add <server> telegram --token … --chat-id …` | alerts to Telegram |
| `moin alerts add <server> slack --url …` | alerts to Slack. `discord` and `webhook` take `--url` too |
| `moin alerts rm <server> <n>` | remove one, by its number in `moin alerts` |
| `moin alerts test <server>` | send a test alert |

[Security](/docs/security#the-team)

## Watcher

| Command | What it does |
| --- | --- |
| `moin watcher run` | run a watcher, on another machine |
| `moin watcher token <server>` | on the watcher: a token for a server, and the line to paste on your laptop |
| `moin watcher connect <server> <url> <token>` | on your laptop: the server reports to that watcher |
| `moin watcher disconnect <server> <url>` | the server stops reporting there |

[Watcher](/docs/watcher)

## The server

| Command | What it does |
| --- | --- |
| `moin secure <server>` | web through Cloudflare Tunnel, a firewall around the rest |
| `moin unlock` | on the server, from your provider's console: open public SSH again |
| `moin update` | update the CLI on your laptop |
| `moin update <server>` | update the server to the version of your CLI |
| `moin version` | the version |
| `moin help` | every command |

## Asking first

A command that deletes or replaces something asks first, like a restore or `moin keys rm`. On a terminal you answer `y`. In a script it needs `-y`, or it stops.

## For scripts: --json

Every command that reads takes `--json`, and prints JSON instead of a table:

```bash
moin --json
moin backups shop --json
```

A command that fails exits with a code other than 0, and says why.

## AI agents

There is no MCP server, and none is needed. AI coding agents like Claude Code, Codex and Cursor use the CLI like you do.

- They read with `--json`.
- Anything that deletes needs `-y`, so it shows in the agent's transcript.
- They run with your SSH key, so they can do what you can. Give an agent what you would give a colleague.

Put this in your project's `AGENTS.md` or `CLAUDE.md`:

```markdown
Deploy with `moin deploy prod`. Check with `moin --json`.
```
