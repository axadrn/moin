---
title: "Deploy"
description: "Deploy, redeploy, restart, roll back and remove apps"
group: "Apps"
order: 1
---

A deploy puts a new version of your app live: v1, then v2, then v3. If it fails, the last good deploy keeps running.

## Three questions

There are many ways to deploy. They are only three questions.

```text
Where does the code come from?
  your folder    moin deploy
  GitHub         git push
  CI             a finished image

Who presses the button?
  CLI, TUI, web, API or MCP

Where do settings live?
  in the repo (x-moin), which wins
  otherwise in moin
```

All of them work. Mix them as you like.

| Code from | Button | Needs a bucket |
| --- | --- | --- |
| [Your folder](#deploy-from-your-folder) | CLI, TUI, API | yes |
| [GitHub](/docs/guides/github) | every push by itself, or web, CLI, TUI, API, MCP | no |
| [CI](/docs/guides/ci) | a deploy hook, or web, CLI, TUI, API, MCP | no |

The web and MCP cannot reach the files on your computer. Everything else does the same work.

Settings: [who decides](/docs/guides/config#who-decides).

## Deploy from your folder

moin takes the code in your folder to the server. The server builds it and starts it.

```surfaces
CLI: moin deploy
TUI: `D` on the overview or the app
API: POST /api/v1/apps/{app}/uploads
API: POST /api/v1/apps/{app}/deploys
```

```bash
moin deploy                                                  # the app of this folder
moin deploy <app-name>                                       # a named app
moin deploy --server <server-name> --dir ./site <app-name>   # a new app, from another folder
```

If the app does not exist, moin asks first: `[y/N]`. So a typo makes no new app. Scripts add `--yes`. The moin GitHub Action passes it for you.

You need no Dockerfile. moin uses your `compose.yml` or `Dockerfile`, or builds it by itself. Your app listens on the port in `PORT`, which is 8080 unless you say otherwise. [How moin builds a repo](/docs/concepts/how-moin-works#how-moin-builds-a-repo).

### No Dockerfile

Without a `Dockerfile` or `compose.yml`, moin builds the repo with [Railpack](https://railpack.com). The deploy says what it found:

```text
→ Railpack: Node 24.22.1, start: npm run start
```

Set your own start command with the env var `RAILPACK_START_CMD`. `DATA_DIR=/data` is set for you.

If Railpack cannot tell how to build the repo, the deploy fails and says why. Then add a `Dockerfile` or a `compose.yml`.

## What a deploy does

```text
your code   from a folder, GitHub
   │        or CI
   ▼
build       on your server;
   │        v2 keeps running
   ▼
start       v3 takes v2's place
   │
   ▼
check       v3 runs 10 seconds
   │        without a restart; the
   │        health path, if set,
   │        answers
   ▼
live ✓      v3 is live

build fails: v2 never stopped.
start or check fails: v2 starts
again, v3 is marked failed.
the server restarts or the daemon
dies halfway: the same, as soon as
the daemon is back.
```

Step by step: [What a deploy does](/docs/concepts/how-moin-works#what-a-deploy-does).

## See your apps

```surfaces
Web: **Apps**
CLI: moin apps
TUI: the overview, where `moin` starts
API: GET /api/v1/apps
MCP: list_apps
```

## Create an app

An empty app on a [server](/docs/guides/servers), ready for its first deploy.

```surfaces
Web: **Apps**, **Add app**
CLI: moin apps add --server <server-name> <app-name>
TUI: opens the web: `w`, then `p`
API: POST /api/v1/apps
MCP: create_app
```

## Follow a deploy

```surfaces
Web: the app, **Deploys**, the deploy log
CLI: moin deploy
TUI: opens by itself when a deploy starts
API: GET /api/v1/apps/{app}/deploys/{number}/events
MCP: get_deploy
```

The web follows every new deploy by itself. An AI agent calls `get_deploy` again until it is done.

## See past deploys

Every deploy keeps its log. A failed log ends with the last lines of the service that stopped. Start there.

```surfaces
Web: the app, **Deploys**, click a deploy
CLI: moin deploys <app-name>
CLI: moin deploys v3
TUI: on the app, `d`, pick the deploy, then `enter`
API: GET /api/v1/apps/{app}
API: GET /api/v1/apps/{app}/deploys/{number}
MCP: get_app
MCP: get_deploy
```

## Redeploy

Builds the newest code again, with the env vars as they are now. Nothing is uploaded. An image deploy pulls the image again.

```surfaces
Web: the app, **Redeploy**
CLI: moin redeploy <app-name>
TUI: on the app, `R`, then `y`
API: POST /api/v1/apps/{app}/redeploy
MCP: redeploy_app
```

## Restart

Starts the live deploy again, without a build. It applies your [env vars](/docs/guides/env), [domains and password](/docs/guides/domains) as they are now.

Saving a setting never restarts an app. The changes wait until a restart. The app page says "2 changes waiting". The API calls it `changes_waiting`.

```surfaces
Web: the app, **Restart**
CLI: moin restart <app-name>
TUI: on the app, `T`, then `y`
API: POST /api/v1/apps/{app}/restart
MCP: restart_app
```

Images gone from the server? The restart builds them again from the source the deploy kept. If that is gone too, it says so. Then deploy again.

## Stop and start

Stop takes the app offline. It stays off, also after a reboot. Its data and settings stay.

Start runs it again. A deploy starts it too.

```surfaces
Web: the app, **Stop** or **Start**
CLI: moin stop <app-name>
CLI: moin start <app-name>
TUI: on the app, `S` (stop asks `y`)
API: POST /api/v1/apps/{app}/stop
API: POST /api/v1/apps/{app}/start
MCP: stop_app
MCP: start_app
```

## Roll back

Starts an earlier good deploy again, with the env vars it had.

```surfaces
Web: the app, **Deploys**, **Roll back** on a deploy
CLI: moin rollback v3
TUI: on the app, `d`, pick the deploy, `r`, then `y`; `P` rolls back to the one before the live one
API: POST /api/v1/apps/{app}/rollback
MCP: rollback_app
```

A rollback is a new deploy, such as v4 with the note *rollback to v3*. Without a number, it takes the good deploy before the live one. The API takes `{"number": 3}`. The server keeps the last 5 builds.

## Pin your apps

Pinned apps stay at the top of the overview, right under what needs you. A pin is for the whole workspace.

```surfaces
Web: the star next to the app's name
CLI: moin apps pin <app-name>, moin apps unpin <app-name>
TUI: on the app, `*`
API: PUT /api/v1/apps/{app}/pin {"pinned": true}
MCP: pin_app
```

## Remove an app

Stops the app and removes its containers and images. Its volumes and data stay on the server. Its backups stay in your bucket until you delete them: [How long things stay](/docs/concepts/backups#how-long-things-stay).

```surfaces
Web: the app, **Remove app**
CLI: moin apps rm <app-name>
TUI: opens the web: `w`, then `p`
API: DELETE /api/v1/apps/{app}
MCP: remove_app
```

You type the app's name to confirm, and the hub checks it. A script passes it as `--confirm <app>`, the API as `?confirm=<app>`, an AI agent as `confirm`.

## Good to know

- Commands on an app take its name as the last word. Without it, moin uses the app of your folder: `app` in [x-moin](/docs/guides/config), or the folder's name.
- moin never stores your code. The archive passes through your bucket and is gone once the server has it.
- Your data survives every deploy: [how](/docs/concepts/how-moin-works#your-data-survives-every-deploy).
