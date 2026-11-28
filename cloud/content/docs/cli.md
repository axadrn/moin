---
title: "CLI"
description: "Every moin command in one pattern. The terminal UI. Name the app and moin finds its server. --json on every reading command for scripts and AI agents."
group: "Reference"
order: 1
---

`moin` is one program. On your laptop it is the CLI and the terminal UI. On a server it does the work. `moin help` lists every command, and every command shows its flags with `--help`.

## One pattern

1. **Words without dashes are the command.** A list is a plural noun: `moin domains`. A change is a noun and a verb: `moin domains add`. An action is a verb: `moin deploy`.
2. **Then what it acts on**: the app, the server, the domain. `moin domains add my-app my-app.example.com`.
3. **Everything with `--` is optional.** Leave it out and the sensible thing happens. Only three have a short form: `-s` (`--server`), `-f` (`--follow`), `-y` (`--yes`).

Flags may come before or after the words: `moin logs my-app -f` is `moin logs -f my-app`.

## Name the app from any folder

You name the app: `moin logs my-app`. moin finds the server it runs on. In the app's folder you may leave the name out: `moin logs`.

- The same app on two servers, like staging and production? moin asks for `-s production`. It never guesses.
- A server is a name from your SSH config, like `my-server`, or `user@host`. If `ssh my-server` works, moin works.
- moin keeps your servers in `~/.config/moin/servers`, one per line, and which app runs where in `~/.config/moin/apps.json`. On a miss, it asks every server.

## Leave a value out

Leave a value out on a terminal and moin asks. It shows a list to pick from, like your servers, an alert kind, or a registry. Or it asks a question, like the key file, the domain or the watcher token. In scripts, pass the flags. Without a terminal moin stops and names the one to pass.

## The terminal UI

`moin` alone, on a terminal, opens the terminal UI. Every server is asked again every 5 seconds.

```bash
moin
```

It has three tabs. The top line says which one is open and what needs you.

- **Apps**: every app, by server, with its state and release. On the right: its address, its deploys and its backups.
- **Servers**: every server, with its apps, its watchers, its SSH and its knocks.
- **Backups**: every app with a database, its copies and the last restore check.

Pick an app and press Enter: its logs, following. `esc` goes back.

| Key | What it does |
| --- | --- |
| `↑` `↓`, or `k` `j` | select |
| `tab`, or `1` `2` `3` | the next tab: Apps, Servers, Backups |
| `enter` | an app: its logs. A server: its apps. A copy: restore it |
| `esc` | back from the logs |
| `r` | restart the app, after you say `y` |
| `b` | go back to an older deploy: pick one, then say `y` |
| `c` | a copy of the app's databases now |
| `R` | put a database back to a copy: pick one, then say `y` |
| `o` | open the app's address in your browser |
| `C` | the compose file moin runs, in your `$PAGER` |
| `?` | the keys. Any key closes them |
| `q` | quit |

Each step that changes a server asks first, then shows a spinner. The same actions are the commands below. Deploy from the app's folder with `moin deploy`.

Before your first deploy, `moin` shows three steps: `cd my-app`, `moin deploy <server>`, `moin`. Without a terminal, `moin` prints the table instead. So does `moin apps`.

## Status

| Command | What it does |
| --- | --- |
| `moin` | the terminal UI, or a table of every server and app, and what needs you |
| `moin apps` | the table: every app on every server, its release, its state and its address |
| `moin apps rm <app>` | remove the app: its containers, images, env and domains. Its volumes and copies stay. It asks first. [Remove an app](/docs/deploy#remove-an-app) |
| `moin servers` | your servers, the moin version on each, and the watchers each one reports to |
| `moin servers add <server>` | add a server you deployed to before, like on a new laptop. A first deploy adds it too |
| `moin servers rm <server>` | take a server off your list. Nothing changes on it |

Under the table, `moin` says what needs you. Each thing is one line with a `!`, and under it the command that settles it:

```text
SERVER      APP      DEPLOY   STATE       DOMAIN
my-server   my-app   v4       ● running   https://my-app.203-0-113-7.sslip.io

! my-app: no backup yet.
  moin bucket set my-server
! my-server: port 22 is open to the internet.
  moin secure my-server
! my-server reports to no watcher. Nobody notices when it is gone.
  moin watcher connect my-server <url> <token>
```

- An app that does not run, an app with no backup yet, a daily check that did not prove today's copy, a backup key you have not made or confirmed.
- Port 22 open to the internet, Tailscale that does not run, a Tailscale key that expires within two weeks.
- A server that no watcher watches, or one that does not answer.
- Muted: the knocks of the day. [Knocks](/docs/security#knocks)

In the terminal UI the same lines show on the Servers tab.

## Deploy

| Command | What it does |
| --- | --- |
| `moin deploy my-server` | the first deploy of the app in this folder, to `my-server`. Sets up moin there first |
| `moin deploy` | deploy again, to the server the app runs on |
| `moin deploy my-server --image ghcr.io/you/my-app:1.4` | the server pulls this image from its registry instead of building. [Deploy an image](/docs/deploy#deploy-an-image-from-your-registry) |
| `moin deploy my-server --image my-app:dev` | an image only your laptop has is sent over SSH |
| `moin deploy my-server --catalog umami` | an app from the [catalog](/docs/catalog) |
| `moin deploy --app my-app` | another name for the app. Without it, the app is named after the folder |
| `moin deploy --no-copy` | deploy even when the copy of the databases just before it fails |
| `moin deploys [app]` | every deploy of the app: from where, what it runs, how it ended, how long it took. [Every deploy](/docs/deploy#every-deploy) |
| `moin deploys [app] v7` | what deploy v7 printed |
| `moin rollback [app]` | pick an older release to run again. `--to v3` names it. Without a terminal, the one before |
| `moin restart [app]` | start the live release again, with its env and domains as they are now |
| `moin stop [app]` | stop the app. It stays stopped after a reboot, until `moin start`. Its data stays |
| `moin start [app]` | start a stopped app |
| `moin logs [app]` | the last 200 lines. `-f` follows them, `--tail 1000` shows more. `--json` prints them as a list of time, container and line |
| `moin exec [app] -- <command>` | run one command in the app's container. `--service db` picks another service |
| `moin follow [app] --branch main` | the server deploys each new commit of the branch, read with a deploy key. `--off` stops it. [Deploy from CI](/docs/deploy#deploy-from-ci) |
| `moin follow [app] --image ghcr.io/you/my-app:main` | the server deploys each new image your CI pushes under the tag. `--off` stops it. [Deploy after your tests pass](/docs/deploy#deploy-after-your-tests-pass) |
| `moin catalog` | the apps of the catalog |
| `moin compose [app]` | the compose file moin runs, without a value of the env: `moin compose my-app > compose.yml`. [Run it elsewhere](/docs/deploy#run-it-elsewhere) |

The first deploy to a server asks first: `Set up moin on my-server and deploy my-app? [y/N]`. In a script, add `-y`. [Deploy](/docs/deploy)

## Env vars and domains

| Command | What it does |
| --- | --- |
| `moin env [app]` | the names of the env vars, never the values |
| `moin env edit [app]` | open `$EDITOR` with the values. The app restarts with them |
| `moin env set [app] <NAME>` | set one. The value is asked without showing it, or read from stdin. Never an argument |
| `moin env push [app] [file\|-]` | replace the env with a `.env` file, or stdin with `-`. Lists the names that change and asks first |
| `moin domains [app]` | the app's domains |
| `moin domains add [app] <domain>` | add a domain, with HTTPS |
| `moin domains rm [app] <domain>` | remove one. It asks first |

## Backups

| Command | What it does |
| --- | --- |
| `moin backups [app]` | the copies in your bucket and in moin cloud, one table with WHERE. The last restore check, and which copy it proved |
| `moin backups [app] --every 1h --keep 30d` | how often the app is copied, and how long each copy stays. Give one or both. With `-s <server>` and no app: the server's schedule. [How often](/docs/backups#how-often) |
| `moin backup [app]` | a copy now |
| `moin backup download [app]` | the newest copy as a file on your laptop, opened with your backup key, which moin asks for. `--to <copy>` another, `-o file` where, `--key-file` the key from a file. [Download a copy](/docs/backups#download-a-copy) |
| `moin restore [app]` | pick a copy to go back to, from the bucket or moin cloud, or the undo of the last restore. A copy in moin cloud asks for your backup key |
| `moin restore [app] --to <copy>` | the same, in a script: a copy from `moin backups`, a time like `2026-11-27T14:07` in UTC, or `undo-…` |
| `moin backups [app] --from <server>` | the copies another server made, like one that is gone |
| `moin restore [app] --from <server> --key` | restore one of them. moin asks for your backup key |
| `moin bucket <server>` | the bucket of the server, never its secret, and whether your backup key is confirmed |
| `moin bucket set <server>` | set the bucket. moin asks for each value, then shows your backup key once |
| `moin bucket key <server>` | a new backup key of yours. `--public age1…` takes one you have |
| `moin bucket confirm <server>` | paste your backup key. moin opens a fresh copy with it, on your laptop |

An app with several databases: `moin restore` asks which one, `--database <name>` names it.

In a script, `moin bucket set` takes `--endpoint`, `--bucket` and `--access-key`, and reads the secret key from stdin. `--region` and `--path` are optional. Then confirm the key with `moin bucket confirm`. [Backups](/docs/backups#the-bucket)

## The team and alerts

| Command | What it does |
| --- | --- |
| `moin keys <server>` | the SSH keys that may log in as root: your team |
| `moin keys add <server> <file.pub>` | let one more key in |
| `moin keys rm <server> <name>` | take a key out, by its name or its fingerprint |
| `moin registry <server>` | the registries the server is logged in to, for private images. Never a token |
| `moin registry set <server> <registry> <user>` | log the server in. The token is asked, or read from stdin |
| `moin registry rm <server> <registry>` | log the server out |
| `moin alerts <server>` | where the server sends its alerts, numbered |
| `moin alerts add <server> telegram --token … --chat-id …` | alerts to Telegram |
| `moin alerts add <server> slack --url …` | alerts to Slack. `discord` takes `--url` too |
| `moin alerts add <server> webhook --url …` | alerts as JSON to your own URL, signed. Prints the signing secret once. [Webhooks](/docs/watcher#webhooks) |
| `moin alerts add <server> email --to … --smtp host:port --user …` | alerts by email, through your SMTP server (STARTTLS, or TLS on port 465). The password is asked, or read from stdin. `--from` when the user is not an address |
| `moin alerts rm <server> <n>` | remove one, by its number in `moin alerts` |
| `moin alerts test <server>` | send a test alert to each |

`moin alerts add` sends a test alert first, and keeps it only when that went out. [Security](/docs/security#the-team)

## Watcher

| Command | What it does |
| --- | --- |
| `moin watcher` | each server's watchers. For moin cloud also your plan, the trial days left and the billing link |
| `moin watcher connect <server>` | the server reports to moin cloud. The first time, the browser signs this laptop in |
| `moin watcher connect <server> <url> <token>` | the server reports to a watcher you run |
| `moin watcher disconnect <server> [url]` | the server stops reporting there. Without an address: moin cloud. It asks first |
| `moin watcher logout` | sign this laptop out of moin cloud. The servers keep reporting. [Sessions](/docs/cloud#sessions) |
| `moin watcher run` | run a watcher, on another machine than your servers |
| `moin watcher token <server>` | on the watcher: a token for a server, and the exact line to paste on your laptop |

[Watcher](/docs/watcher), [moin cloud](/docs/cloud)

[Watcher](/docs/watcher)

## Updates and the server

| Command | What it does |
| --- | --- |
| `moin update` | update the CLI on your laptop to the newest release |
| `moin update <server>` | put the version of your CLI on the server |
| `moin daemon uninstall` | on the server: remove moin. The apps keep running |
| `moin daemon uninstall --apps` | on the server: remove moin and every app, with its data |
| `moin version` | the version. `--json` prints it as JSON |
| `moin help` | every command |

## Security

| Command | What it does |
| --- | --- |
| `moin secure <server>` | SSH only over Tailscale, the web through a Cloudflare Tunnel, a firewall for the rest |
| `moin secure <server> --tunnel off` | the web on ports 80 and 443 again, not through the tunnel |
| `moin unlock` | on the server, in your provider's web console: open port 22 again |
| `moin knocks [server]` | the connection attempts the firewall dropped, by port |

[Security](/docs/security)

## Remove moin from a server

Log in to the server, and run it there as root:

```bash
ssh -t my-server
moin daemon uninstall
```

```text
? Remove moin from this server? The apps keep running. [y/N] y
→ stopping the moin-daemon service
→ removing /usr/local/bin/moin
✓ moin is removed from this server
  Still here: the apps my-app with their data in /var/lib/moin/apps; the proxy container moin-traefik, and the tunnel when it was on; the config in /etc/moin/config.yml, which holds the keys of the bucket, and the server's backup key next to it; Docker.
  Remove the apps and their data too, as root, with moin daemon uninstall --apps
```

- Without `--apps`: the daemon goes, and with it the backups, the restore checks and the reports. The apps keep running, with their data and HTTPS.
- Without `--apps`, public port 22 opens again and the firewall goes: `moin unlock` goes with moin, so nothing may stay closed behind it.
- With `--apps`: every app goes too, with its data, its images, the proxy, the tunnel and the firewall. So do `/etc/moin/config.yml` and the server's backup key. Your own backup key still opens the copies in the bucket. This can't be undone.
- Docker stays either way.
- In a script, add `-y`.

Then, on your laptop: `moin watcher disconnect my-server <url>` for each watcher, or it says the server went silent. And `moin servers rm my-server`.

## Asking first

A command that deletes or replaces something asks first, like a restore, `moin keys rm`, `moin registry rm` or `moin domains rm`. On a terminal you answer `y`. In a script it needs `-y`, or it stops.

## For scripts: --json

Every command that reads takes `--json`, and prints JSON instead of a table:

```bash
moin --json
moin backups my-app --json
```

`moin deploy`, `moin rollback`, `moin backup` and `moin restore` take it too. Then the log goes to stderr, and stdout holds only the JSON.

`moin logs --json` prints the lines there are. It takes no `-f`.

A command that fails exits with a code other than 0, and says why. `moin exec` exits with the code of the command it ran.

## New versions

Once a day, the CLI asks GitHub for the newest release. When one is out, it says so above the command: `moin v1.2.4 is out, you run v1.2.3.` It never updates by itself. [Updates](/docs/security#updates)

## AI agents

There is no MCP server, and none is needed. AI coding agents like Claude Code, Codex and Cursor use the CLI like you do.

- They read with `--json`.
- Anything that deletes needs `-y`, so it shows in the agent's transcript.
- They run with your SSH key, so they can do what you can. Give an agent what you would give a colleague. [moin for agents](/docs/agents)

Put this in your project's `AGENTS.md` or `CLAUDE.md`:

```markdown
Deploy with `moin deploy my-server`. Check with `moin --json`.
```
