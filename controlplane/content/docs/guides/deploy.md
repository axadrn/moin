---
title: "Deploy"
description: "Deploy, redeploy and roll back apps"
order: 3
---

## Deploy from a repo

```bash
ops deploy               # the app from x-ops.app or the directory name
ops deploy blog          # a named app
ops deploy --server web-1 --dir ./site blog
```

ops packs the files git knows, including uncommitted changes and new files that are not ignored. Outside a git repo it packs everything but `.git`. The archive goes straight into [your bucket](/docs/guides/bucket); the agent downloads it there, deletes it and builds and starts it with Docker Compose on the app's server. ops never stores your code.

What the repo brings decides how:

| The repo has | ops does |
| --- | --- |
| `compose.yml` | uses it |
| only a `Dockerfile` | runs it as service `web`, mounts `./data` at `/data` |
| a Go module | also writes the Dockerfile: it builds `.`, `./cmd/server` or the only package under `cmd/`, and sets `PORT` and `DATA_DIR=/data` |

## What a deploy does

1. Downloads the source from your bucket, or the commit from GitHub, checks it and keeps it with the release.
2. Reads the compose file through `docker compose config`, with the [x-ops block](/docs/guides/config).
3. Tags the built images per deploy, sets the env vars and `restart: unless-stopped`.
4. Builds, starts with `docker compose up --wait` and checks that the main service still runs 3 seconds later.
5. Calls the health path, if x-ops sets one, until it answers below 500.
6. Reports the SQLite databases it finds on the app's mounts.

If the start fails, the previous release starts again and the deploy is **failed**. The server keeps the last 5 releases and their images.

## Your data survives every deploy

A bind mount whose path the repo does not bring, like `./data`, moves to `/var/lib/ops/apps/<app>/volumes/` and stays there across deploys. Named volumes are kept as they are. Paths the repo brings, like a config file, belong to the release.

## Follow a deploy

| | Web | CLI | TUI | MCP |
| --- | --- | --- | --- | --- |
| Deploy the current repo | – | `ops deploy` | `d` | – |
| Live log | app page, **Deploy log** | printed by `ops deploy` | output panel | `get_deploy`, poll it |
| Deploy history | app page, **Deploys** | – | `enter` on the app | `get_app` |

The web page follows every new deploy by itself, also the ones started from the CLI.

## Redeploy

Builds the newest source again, from the copy the server kept, with the env vars as they are now. Nothing is uploaded. Use it after changing env vars.

| Web | CLI | TUI | MCP |
| --- | --- | --- | --- |
| **Redeploy** | `ops redeploy [app]` | `R` | `redeploy_app` |

## Roll back

Starts the images of an earlier successful deploy again, with the env vars that deploy had. It is a new deploy, for example v4 with the note *rollback to v1*.

| Web | CLI | TUI | MCP |
| --- | --- | --- | --- |
| **Roll back** on a deploy | `ops rollback` or `ops rollback v3` | `r`, then `y` | `rollback_app` |

Without a number it picks the successful deploy before the live one.

## Remove an app

Stops it and removes its containers and images. Volumes and data stay on the server.

| Web | CLI | TUI | MCP |
| --- | --- | --- | --- |
| **Remove** | `ops apps rm <app>` | `x`, then `y` | `remove_app` |

## Push to deploy with GitHub Actions

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
