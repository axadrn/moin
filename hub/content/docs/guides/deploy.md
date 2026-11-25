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
  GitHub         git push, then moin deploy
  CI             a finished image
  the catalog    an app by its name

Who presses the button?
  CLI, TUI, web, API or MCP

Where do settings live?
  in moin, always
```

All of them work. Mix them as you like.

| Code from | Button |
| --- | --- |
| [GitHub](#deploy-what-you-pushed) | every push by itself, `moin deploy` in the repo, or web, CLI, TUI, API, MCP |
| [CI](/docs/guides/ci) | a deploy hook, or web, CLI, TUI, API, MCP |
| [The catalog](/docs/guides/catalog) | web, CLI, API, MCP |

Code never comes from your computer. Your server builds what is on GitHub. So what runs is what you pushed.

## Deploy from

An app deploys from one place at a time: GitHub or CI. The app's **Deploys** tab shows it as **Deploy from**, with a green dot on the one in use. Pick another to switch. moin asks first, because the old one stops.

CI still uses the repo. Each image deploy takes `compose.yml` from the commit the image was built from. So the repo stays linked while CI deploys. Only its pushes no longer deploy.

```surfaces
Web: the app's **Deploys** tab, **Deploy from**
CLI: moin apps <app-name>, the line deploy from
API: GET /api/v1/apps/{app}, the field deploy_from: ci, github, or empty for neither yet
MCP: get_app, deploy_from
```

Switch with what turns one on: [deploy every push](/docs/guides/github#deploy-every-push), [turn on the deploy hook](/docs/guides/ci). Turning the hook off leaves the repo linked, without deploys on push. Unlinking leaves neither. `moin deploy` in the repo links it again.

Settings live in moin. [compose.yml](/docs/guides/compose) is plain Docker.

## Deploy what you pushed

Push your repo to GitHub. Then run `moin deploy` in it. Your server downloads that commit from GitHub, builds it and starts it. The code never passes through moin.

```surfaces
Web: the app's **Deploys** tab, **Deploy from**, **GitHub**, a commit, **Deploy**
CLI: moin deploy
TUI: `D` on the overview or the app
API: POST /api/v1/apps/{app}/deploys {"ref": "<commit>"}
MCP: deploy_ref
```

```bash
moin deploy                                    # the app of this repo
moin deploy <app-name>                         # a named app
moin deploy --server <server-name> <app-name>  # a new app on that server
```

moin reads the repo through the moin GitHub App. [Connect GitHub](/docs/guides/github#connect-github) once.

Before a deploy, moin checks three things. If one fails, it says what to do.

- You are in the top folder of the repo, and its `origin` is on GitHub.
- Everything is committed. `git status` shows nothing.
- The commit is pushed. `git push` first.

The first deploy links the app to the repo, without deploys on push. Turn those on with [`moin link`](/docs/guides/github#link-a-repo). An app linked to another repo deploys nothing from this one.

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
your code   from GitHub, CI
   │        or the catalog
   ▼
build       on your server;
   │        v2 keeps running
   ▼
start       v3 takes v2's place
   │
   ▼
check       v3 runs 10 seconds
   │        without a restart; with
   │        a healthcheck, Docker
   │        says it is healthy
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

Builds the newest code again, with the env vars as they are now. The server builds the code it kept. An image deploy pulls the image again.

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

Starts an earlier good deploy again: its code and images, with the env vars, the domains and the [password](/docs/guides/domains#password) as they are now.

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

- Commands on an app take its name as the last word. Without it, moin takes the app that follows the folder's GitHub repo, else the folder's name. Two apps follow it? Name one.
- moin never stores your code. Your server downloads it from GitHub.
- Your data survives every deploy: [how](/docs/concepts/how-moin-works#your-data-survives-every-deploy).
- A deploy refuses settings that reach beyond the app, like `privileged: true` or a mount of `/var/run/docker.sock`. A normal app needs none of them. [Containers that reach into the server](/docs/guides/security#containers-that-reach-into-the-server)
