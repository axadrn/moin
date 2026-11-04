---
title: "Deploy"
description: "Deploy, redeploy and roll back apps"
order: 3
---

A deploy puts a new version of your app live on its server. moin numbers them per app: v1, v2, v3.

Commands that work on an app take its name as the last word, like `moin redeploy <app-name>`. Leave it out and moin uses the app of the folder you are in: the `app` in [x-moin](/docs/guides/config), or else the folder's name.

## Deploy from a repo

Sends the code in your current folder to the server, which builds and starts it.

```surfaces
CLI: moin deploy
TUI: on the app, `d`
API: POST /api/v1/apps/{app}/uploads
API: POST /api/v1/apps/{app}/deploys
```

The web and MCP cannot reach the files on your computer. They deploy from [GitHub](/docs/guides/github) or an [image](/docs/guides/ci) instead. In the API, upload first, then start the deploy.

```bash
moin deploy                                                  # the app of this folder
moin deploy <app-name>                                       # a named app
moin deploy --server <server-name> --dir ./site <app-name>   # a new app on one of several servers, from another folder
```

moin packs the files git knows, including changes you have not committed and new files that are not ignored. Outside a git repo it packs everything but `.git`. The archive goes straight into [your bucket](/docs/guides/bucket). The agent downloads it from there, deletes it, and builds and starts it with Docker Compose on the app's server. moin never stores your code.

What the repo brings decides how moin builds it:

| The repo has | moin does |
| --- | --- |
| `compose.yml` | uses it |
| only a `Dockerfile` | runs it as service `web`, mounts `./data` at `/data` |
| a Go module | also writes the Dockerfile: it builds `.`, `./cmd/server` or the only package under `cmd/`, and sets `PORT` and `DATA_DIR=/data` |
| none of these | builds it with [Railpack](#no-dockerfile): Node, Bun, Python, Ruby, PHP and more |

Your app must listen on the port moin gives it in `PORT`, or on the port in your compose file or Dockerfile. [Config in the repo](/docs/guides/config) changes the port, the health check and more.

### No Dockerfile

No Dockerfile? No problem. moin reads your repo and builds it with [Railpack](https://railpack.com), the open source builder Railway made: Node, Bun, Python, Ruby, PHP, Deno, Rust, Elixir, Java, .NET, static sites and more.

- **It finds the language and the version**: from `package.json`, `requirements.txt`, `pyproject.toml`, `Gemfile`, `composer.json` and the like.
- **It finds the start command**: the `start` script, a `Procfile`, `main.py`. Set your own with the env var `RAILPACK_START_CMD`.
- **Your app listens on `PORT`**: moin sets it to 8080 and sends traffic there, like Railway.
- **Your env vars reach the build**, as secrets that never land in the image or in a log. A build that needs `NEXT_PUBLIC_API_URL` gets it.
- **Data goes to `/data`**, like any app moin builds: `DATA_DIR=/data` is set.

The deploy says what it found:

```text
→ Railpack: Node 24.22.1, start: npm run start
```

The app page, `moin apps <app-name>`, the TUI, the API and `get_app` show it too. If Railpack cannot tell how to build or start the repo, the deploy fails with its reason; add a `Dockerfile` or a `compose.yml` and deploy again. The agent downloads Railpack once, checked against its published checksum, and the build runs on your server with Docker's own BuildKit.

## See your apps

Lists every app with its server and its live deploy.

```surfaces
Web: **Apps**
CLI: moin apps
TUI: `moin`, then the **apps** tab
API: GET /api/v1/apps
MCP: list_apps
```

## Create an app

Makes an empty app on a [server](/docs/guides/servers), ready for its first deploy. The first `moin deploy` in a repo does this by itself.

```surfaces
Web: **Apps**, **Add app**
CLI: moin apps add --server <server-name> <app-name>
TUI: on the apps tab, `a`
API: POST /api/v1/apps
MCP: create_app
```

## What a deploy does

1. Downloads the code from your bucket, or the commit from GitHub, checks it and keeps it with the release.
2. Reads the compose file with `docker compose config`, together with the [x-moin block](/docs/guides/config).
3. Tags the built images per deploy, sets the env vars and `restart: unless-stopped`.
4. Builds, starts with `docker compose up --wait` and checks that the main service still runs 5 seconds later.
5. Calls the health path, if x-moin sets one, until it answers below 500.
6. Reports the SQLite databases it finds on the app's volumes, for the [data browser](/docs/guides/data) and the [backups](/docs/guides/backups).

If the start fails, the previous release starts again and the deploy is marked **failed**. Its log ends with the last lines of the service that stopped.

The server keeps the releases of the last 5 builds (of code or of an image) with their images, and every restart, start, domain change and rollback since the oldest of them. Roll back can go to any of those.

## Your data survives every deploy

A volume is a folder on the server that your app's container can see, such as `./data` on the server shown as `/data` in the container. A folder your app mounts but the repo does not bring, like `./data`, moves to `/var/lib/moin/apps/<app>/volumes/` and stays there across deploys. Named volumes are kept as they are. Files the repo brings, like a config file, belong to the release.

Your app keeps its database in a folder other than `/data`? Mount that folder in `compose.yml`, for example `./data:/app/data`. moin keeps it and backs up the SQLite files in it.

## Follow a deploy

Shows the log of a deploy while it runs, step by step.

```surfaces
Web: the app page, **Deploy log**
CLI: moin deploy
TUI: the output panel
API: GET /api/v1/apps/{app}/deploys/{number}/events
MCP: get_deploy
```

`moin deploy` prints the log as it goes. An agent over MCP calls `get_deploy` again until the deploy is done. The web page follows every new deploy by itself, also one started from the CLI.

## See past deploys

Lists the deploys of an app, newest first, with their status.

```surfaces
Web: the app page, **Deploys**
CLI: moin deploys <app-name>
TUI: on the app, `enter`
API: GET /api/v1/apps/{app}
MCP: get_app
```

## Read the log of one deploy

Shows the full log of an earlier deploy. Every deploy keeps its log, a failed one too, so you can read later what went wrong. A failed deploy's log ends with the last lines of the service that stopped: start there.

```surfaces
Web: the app page, click the deploy under **Deploys**
CLI: moin deploys v3
TUI: on the app, pick the deploy with `↑` `↓`, then `enter`
API: GET /api/v1/apps/{app}/deploys/{number}
MCP: get_deploy
```

## Redeploy

Builds the newest code again from the copy the server kept, with the env vars as they are now. Nothing is uploaded.

```surfaces
Web: the app page, **Redeploy**
CLI: moin redeploy <app-name>
TUI: on the app, `R`
API: POST /api/v1/apps/{app}/redeploy
MCP: redeploy_app
```

After changing env vars, a restart is enough and faster. An app deployed from an image pulls and runs the newest image again instead.

## Restart

Starts the live release again with the [env vars](/docs/guides/env), [domains and password](/docs/guides/domains) as they are now. Nothing is built. Use it after changing env vars.

If the app's images are gone from the server, for example after `docker image prune`, the restart builds them again from the source the release kept. If that source is gone too, it says so: deploy again.

```surfaces
Web: the app page, **Restart**
CLI: moin restart <app-name>
TUI: on the app, `t`
API: POST /api/v1/apps/{app}/restart
MCP: restart_app
```

## Stop

Takes the app offline. It stays off, also after the server reboots. Its data and settings stay.

```surfaces
Web: the app page, **Stop**
CLI: moin stop <app-name>
TUI: on the app, `S`, then `y`
API: POST /api/v1/apps/{app}/stop
MCP: stop_app
```

`moin stop` asks first; `--yes` skips the question. Changes to the domains or the password of a stopped app wait until it starts.

## Start

Runs a stopped app again, like a restart. A deploy starts it too. An app that runs already stays as it is: moin says it is already running.

```surfaces
Web: the app page, **Start**
CLI: moin start <app-name>
TUI: on the app, `S`
API: POST /api/v1/apps/{app}/start
MCP: start_app
```

## Roll back

Starts an earlier successful deploy again, with the env vars it had. Use it when a new version is broken.

```surfaces
Web: the app page, **Roll back** on a deploy
CLI: moin rollback v3
TUI: on the app, `r`, then `y`
API: POST /api/v1/apps/{app}/rollback
MCP: rollback_app
```

A rollback is a new deploy, for example v4 with the note *rollback to v3*. Without a number (`moin rollback`), it picks the successful deploy before the live one. In the TUI, pick a deploy with `↑` `↓` before `r` to choose one. In the API, send the deploy as `{"number": 3}`. The web, the CLI and the TUI ask first; in a script, add `--yes`.

## Remove an app

Stops the app and removes its containers and images. Its volumes and data stay on the server, and its backups stay in your bucket until you delete them: see [How long things stay](/docs/guides/bucket#how-long-things-stay).

```surfaces
Web: the app page, **Remove app**
CLI: moin apps rm <app-name>
TUI: on the app, `x`, then type its name
API: DELETE /api/v1/apps/{app}
MCP: remove_app
```

You cannot undo it, so moin asks you to type the app's name first. `moin apps rm <app-name> --yes` skips that in scripts. An agent passes the name as `confirm`.

## Order your apps

Apps show in the order you give them, the same for everyone in the workspace and everywhere: the apps page and the sidebar, the CLI, the TUI, the API and MCP. A new app goes to the end.

```surfaces
Web: **Apps**, drag an app to its place
CLI: moin apps move <app-name> up|down|top|bottom|<position>
TUI: on the app list, `K` or `J`, or `:move up` and `:move down`
API: PUT /api/v1/apps/order
MCP: reorder_apps
```

With a mouse you drag the whole app; on a phone you drag the grip at its left, so the page still scrolls. On the keyboard, Alt and the up or down arrow move the app in focus. The API and MCP take every app once, by name or ID, in the new order: `{"order": ["blog", "shop"]}`. The activity records each new order once.

**List** and **Grid** above the apps switch how the page shows them: one row each, or cards side by side. moin remembers your choice for you on every device; the rest of the team keeps theirs.

## Push to deploy with GitHub Actions

This workflow deploys every push to `main`. The [GitHub](/docs/guides/github) guide shows a way without CI, and [Build in CI](/docs/guides/ci) builds the image in CI.

It needs a [bucket](/docs/guides/bucket) and an API token with **Read and write** access. [Create the token](/docs/guides/account#create-an-api-token) and save it in your GitHub repo under **Settings**, **Secrets and variables**, **Actions**, as `MOIN_TOKEN`.

```yaml
name: deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go install github.com/axadrn/moin/cmd/moin@latest
      - run: moin deploy
        env:
          MOIN_URL: https://moin.example.com
          MOIN_TOKEN: ${{ secrets.MOIN_TOKEN }}
```
