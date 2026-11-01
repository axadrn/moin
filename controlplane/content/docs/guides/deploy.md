---
title: "Deploy"
description: "Deploy, redeploy and roll back apps"
order: 3
---

A deploy puts a new version of your app live on its server. ops numbers them per app: v1, v2, v3.

## Deploy from a repo

Sends the code in your current folder to the server, which builds and starts it.

```surfaces
CLI: ops deploy
TUI: on the app, `d`
API: POST /api/v1/apps/{app}/uploads
API: POST /api/v1/apps/{app}/deploys
```

The web and MCP cannot reach the files on your computer. They deploy from [GitHub](/docs/guides/github) or an [image](/docs/guides/ci) instead. In the API, upload first, then start the deploy.

```bash
ops deploy               # the app from x-ops.app or the folder name
ops deploy blog          # a named app
ops deploy --server web-1 --dir ./site blog
```

ops packs the files git knows, including changes you have not committed and new files that are not ignored. Outside a git repo it packs everything but `.git`. The archive goes straight into [your bucket](/docs/guides/bucket). The agent downloads it from there, deletes it, and builds and starts it with Docker Compose on the app's server. ops never stores your code.

What the repo brings decides how ops builds it:

| The repo has | ops does |
| --- | --- |
| `compose.yml` | uses it |
| only a `Dockerfile` | runs it as service `web`, mounts `./data` at `/data` |
| a Go module | also writes the Dockerfile: it builds `.`, `./cmd/server` or the only package under `cmd/`, and sets `PORT` and `DATA_DIR=/data` |

## Create an app

Makes an empty app on a server, ready for its first deploy. The first `ops deploy` in a repo does this by itself.

```surfaces
Web: **Apps**, **Add app**
CLI: ops apps add --server web-1 blog
TUI: on the apps tab, `a`
API: POST /api/v1/apps
MCP: create_app
```

## What a deploy does

1. Downloads the code from your bucket, or the commit from GitHub, checks it and keeps it with the release.
2. Reads the compose file with `docker compose config`, together with the [x-ops block](/docs/guides/config).
3. Tags the built images per deploy, sets the env vars and `restart: unless-stopped`.
4. Builds, starts with `docker compose up --wait` and checks that the main service still runs 5 seconds later.
5. Calls the health path, if x-ops sets one, until it answers below 500.
6. Reports the SQLite databases it finds on the app's volumes.

If the start fails, the previous release starts again and the deploy is marked **failed**. Its log ends with the last lines of the service that stopped.

The server keeps the releases of the last 5 builds (of code or of an image) with their images, and every restart, start, domain change and rollback since the oldest of them. Roll back can go to any of those.

## Your data survives every deploy

A folder your app mounts but the repo does not bring, like `./data`, moves to `/var/lib/ops/apps/<app>/volumes/` and stays there across deploys. Named volumes are kept as they are. Files the repo brings, like a config file, belong to the release.

Your app keeps its database in a folder other than `/data`? Mount that folder in `compose.yml`, for example `./data:/app/data`. ops keeps it and backs up the SQLite files in it.

## Follow a deploy

Shows the log of a deploy while it runs, step by step.

```surfaces
Web: the app page, **Deploy log**
CLI: ops deploy
TUI: the output panel
API: GET /api/v1/apps/{app}/deploys/{number}/events
MCP: get_deploy
```

`ops deploy` prints the log as it goes. An agent over MCP calls `get_deploy` again until the deploy is done. The web page follows every new deploy by itself, also one started from the CLI.

## See past deploys

Lists the deploys of an app, newest first, with their status.

```surfaces
Web: the app page, **Deploys**
CLI: ops deploys blog
TUI: on the app, `enter`
API: GET /api/v1/apps/{app}
MCP: get_app
```

## Read the log of one deploy

Shows the full log of an earlier deploy. Every deploy keeps its log, a failed one too, so you can read later what went wrong.

```surfaces
Web: the app page, click the deploy under **Deploys**
CLI: ops deploys v3
TUI: on the app, pick the deploy with `↑` `↓`, then `enter`
API: GET /api/v1/apps/{app}/deploys/{number}
MCP: get_deploy
```

## Redeploy

Builds the newest code again from the copy the server kept, with the env vars as they are now. Nothing is uploaded.

```surfaces
Web: the app page, **Redeploy**
CLI: ops redeploy blog
TUI: on the app, `R`
API: POST /api/v1/apps/{app}/redeploy
MCP: redeploy_app
```

After changing env vars, a restart is enough and faster. An app deployed from an image pulls and runs the newest image again instead.

## Restart

Starts the live release again with the env vars, domains and password as they are now. Nothing is built. Use it after changing env vars.

```surfaces
Web: the app page, **Restart**
CLI: ops restart blog
TUI: on the app, `t`
API: POST /api/v1/apps/{app}/restart
MCP: restart_app
```

## Stop

Takes the app offline. It stays off, also after the server reboots. Its data and settings stay.

```surfaces
Web: the app page, **Stop**
CLI: ops stop blog
TUI: on the app, `S`, then `y`
API: POST /api/v1/apps/{app}/stop
MCP: stop_app
```

`ops stop` asks first; `--yes` skips the question. Changes to the domains or the password of a stopped app wait until it starts.

## Start

Runs a stopped app again, like a restart. A deploy starts it too.

```surfaces
Web: the app page, **Start**
CLI: ops start blog
TUI: on the app, `S`
API: POST /api/v1/apps/{app}/start
MCP: start_app
```

## Roll back

Starts an earlier successful deploy again, with the env vars it had. Use it when a new version is broken.

```surfaces
Web: the app page, **Roll back** on a deploy
CLI: ops rollback v3
TUI: on the app, `r`, then `y`
API: POST /api/v1/apps/{app}/rollback
MCP: rollback_app
```

A rollback is a new deploy, for example v4 with the note *rollback to v3*. Without a number (`ops rollback`), it picks the successful deploy before the live one. In the TUI, pick a deploy with `↑` `↓` before `r` to choose one. In the API, send the deploy as `{"number": 3}`.

## Remove an app

Stops the app and removes its containers and images. Its volumes and data stay on the server.

```surfaces
Web: the app page, **Remove app**
CLI: ops apps rm blog
TUI: on the app, `x`, then `y`
API: DELETE /api/v1/apps/{app}
MCP: remove_app
```

## Push to deploy with GitHub Actions

This workflow deploys every push to `main`. The [GitHub](/docs/guides/github) guide shows a way without CI, and [Build in CI](/docs/guides/ci) builds the image in CI.

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
      - run: go install github.com/axadrn/ops/cmd/ops@latest
      - run: ops deploy
        env:
          OPS_URL: https://ops.example.com
          OPS_TOKEN: ${{ secrets.OPS_TOKEN }}
```
