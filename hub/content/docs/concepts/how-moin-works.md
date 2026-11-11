---
title: "How moin works"
description: "The pieces, the words, where your code goes and what a deploy does"
group: "Start"
order: 4
---

moin has two halves. The hub is the place you sign in to. The daemon is the little helper on each of your servers. It does the work.

Your code and your backups never live with the hub.

## The pieces

| Piece | What it is |
| --- | --- |
| **hub** | The moin web app and API. It remembers your servers, apps and settings, and tells the daemons what to do. We run it, or you [run it yourself](/docs/self-hosting). |
| **daemon** | The little helper on each server. It calls the hub and keeps the line open. Nothing on your server waits for calls. |
| **`moin`** | One program on your computer: `moin` opens the terminal UI, `moin <command>` is the CLI, `moin daemon` is the daemon. |

The web, the CLI, the terminal UI, the [API](/docs/reference/api) and AI agents through [MCP](/docs/guides/mcp) do the same work. [Where things work](/docs/reference/surfaces) lists each one.

```text
workspace: team, bucket, servers
├─ web-1: my-app, blog
└─ web-2: api

your code
  moin deploy ─▶ bucket ─▶ server
  GitHub ────────────────▶ server
  CI image ──────────────▶ server

backups
  server ─▶ your bucket
```

## Who talks to whom

```text
web, CLI, TUI, MCP ──▶ hub ◀── daemon on each server
          the API      │        a line the daemon opens
```

- **You talk to the hub.** The web, the CLI, the terminal UI and AI agents all use the same API.
- **The hub talks to the daemons.** Each daemon calls the hub and keeps one line open. The hub sends its orders down that line.
- **You never talk to a daemon.** Nothing on your server waits for calls.

## What the daemon does

The hub is the brain. The daemon is the hands. The hub decides what happens and when. The daemon does it on the server.

The daemon knows a fixed list of orders, and the code for each is in the daemon itself. The hub can only send orders from that list. It cannot send new code. A new kind of order comes with a new version of moin.

| The daemon | With |
| --- | --- |
| builds, starts, stops and removes your apps | Docker |
| shows logs, runs a command in an app | Docker |
| reads and edits your databases | `psql` in your Postgres container, SQLite directly |
| copies your databases to your bucket, and restores them | your bucket |
| measures CPU, memory and disk | the server |
| routes your domains, with HTTPS | Traefik |
| locks the server down: tunnel, SSH, firewall | the server |
| updates itself | the hub, which serves the new version |

**The rules on your server win.** A file, `/etc/moin/rules`, can turn orders off, such as reading data or running commands. The daemon checks every order against it. The hub cannot change that file. [Rules](/docs/guides/security#rules)

**The daemon runs as root.** It has to: it runs Docker, and whoever runs Docker can do what root can. That is why the list of orders is fixed and the rules file is yours.

## The words

The first five are in [moin in five words](/docs/moin-in-five-words). The rest:

| Word | Means |
| --- | --- |
| **workspace** | your own space: apps, servers, bucket and team. You get one when you sign up. On hosted moin you pay per workspace |
| **team** | the people in a workspace. [Roles](/docs/guides/team#roles) |
| **service** | one part of an app, such as its website or its database: one container of its `compose.yml` |
| **restart** | starts the live deploy again, so the changes that wait take effect |
| **domain** | the name people type to reach your app, such as `my-app.example.com`, always with HTTPS |
| **env vars** | the settings and secrets of an app, kept encrypted |
| **restore** | going back to a copy. It can be undone |
| **health check** | after a deploy, moin checks that the app starts and stays up |
| **catalog** | the place with apps you install in one click |
| **API token** | a key for the CLI, scripts and AI agents, for one workspace |
| **MCP** | the way AI agents use moin |
| **activity** | who did what, and when |

Your app runs in containers, with Docker. You do not need to know Docker. For a `Dockerfile`, a Go module or a repo with neither, moin writes the compose file for you.

## Saving and restarting

Saving a setting never restarts your app. The change waits for you. The app page says how many changes wait.

A restart applies them all, without a build. A deploy or a start applies them too. A change to a variable used as `${NAME}` in `compose.yml` takes a deploy.

## Your data survives every deploy

What your app saves stays when you deploy, as long as it lives in a mounted folder.

A bind mount and a volume are both a folder on the server that the container sees. With a bind mount you pick the folder. With a volume Docker picks it.

Take a folder. Volumes work too.

- A folder like `./data` moves to `/var/lib/moin/apps/<app>/volumes/` and stays there across deploys.
- Files your repo brings, like a config file, belong to the deploy.
- Volumes are set in `compose.yml` only. There is no button for them.
- If you rename a volume in `compose.yml`, the app starts on a new empty one.

Keep your database in a mounted folder. If it is not in `/data`, mount its folder in `compose.yml`, such as `./data:/app/data`.

## Under the hood

### Where builds run

Builds run on **your** server.

- `moin deploy` packs the files git knows, with uncommitted changes and new files that are not ignored. Outside a git repo, everything but `.git`.
- A deploy from [GitHub](/docs/guides/github) goes from GitHub straight to your server, with a short-lived token. It needs no bucket.
- A deploy from [CI](/docs/guides/ci) builds the image elsewhere. The server only pulls it.

The hub only passes messages. It never stores or runs your code.

```text
1 computer  moin deploy packs your
     │      folder and puts it into
     ▼      your bucket
2 bucket    keeps it for a moment
     │
     ▼
3 server    the daemon downloads it,
            checks it, deletes it
            from the bucket, builds it
```

moin only signs the links. Your code never passes through moin.

### How moin builds a repo

| The repo has | moin does |
| --- | --- |
| `compose.yml` | uses it (`compose.yaml` and `docker-compose.yml` work too) |
| only a `Dockerfile` | runs it as service `web`, mounts `./data` at `/data` |
| a Go module | writes the Dockerfile too: it builds `.`, `./cmd/server` or the only package under `cmd/`, and sets `PORT` and `DATA_DIR=/data` |
| none of these | builds it with [Railpack](https://railpack.com), the open source builder Railway made |

Railpack finds the language, the version and the start command by itself. It covers Node, Bun, Python, Ruby, PHP, Deno, Rust, Elixir, Java, .NET, static sites and more. Your env vars reach the build as secrets. They never land in the image or a log.

### What a deploy does

1. Gets the code from your bucket or GitHub, checks it and keeps it with the deploy.
2. Reads the compose file and the [x-moin block](/docs/guides/config).
3. Tags the built images, sets the env vars and `restart: unless-stopped`.
4. Builds.
5. Takes a copy of your databases, so **Before v12** can bring them back. The first deploy has none yet.
6. Starts it, and checks that the main service runs for 10 seconds without a restart, within 30 seconds.
7. Calls the health path, if x-moin sets one, until it answers below 500.
8. Reports the databases it found and what it detected. The app page, `moin deploy`, the TUI, the API and `get_app` show it.

If the start or the health check fails, the last good deploy starts again and the new one is marked **failed**. Its log ends with the last lines of the service that stopped.

The server keeps the last 5 builds, with their images. A rollback can go to any of those.

### How moin reads your database

The [data browser](/docs/guides/data) reads the live database on the server. Nothing is copied to the hub.

- For Postgres, moin runs `psql` inside your Postgres container. No password leaves it. Every value you type travels as a psql variable, never as SQL.
- A read opens the database read-only. A statement that would change data fails, so a typo cannot delete rows.
- moin changes data, never the schema. moin names the migration tool and its newest version when it finds a `goose_db_version`, `schema_migrations`, `_prisma_migrations` or `__drizzle_migrations` table.
- WAL mode (`PRAGMA journal_mode=WAL`) lets your app and moin use SQLite at the same time.
- The daemon runs as root. After a change it gives the database files back to their owner.

### How a command runs

[Run a command](/docs/guides/exec) uses the line the daemon already has. No SSH, and no key on your laptop.

### The catalog

Each [catalog](/docs/guides/catalog) app is a folder in the moin repo, `hub/content/catalog/<app>`. moin reads the image registry of each app once an hour. A self-hosted hub ships the same catalog and asks the registries itself. Nothing goes through us.
