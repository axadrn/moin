---
title: "GitHub"
description: "Deploy a branch or commit without a checkout, and every push"
order: 9
---

Connect GitHub once, link a repo to an app, and ops deploys from GitHub: any branch, tag or commit from the web, the CLI, the terminal UI or an agent, with no checkout on your machine. With **Deploy every push** on, every push to the branch deploys. ops writes how it went back to GitHub as the commit status **ops/deploy**, so the pull request shows a green check when the deploy is live.

## Connect GitHub

Installs the ops GitHub App on your account or an organization, so ops may read the repos you choose. You do it once per workspace.

```surfaces
Web: the app page, **GitHub**, **Connect GitHub**
CLI: ops github
TUI: settings tab, **GitHub**, then `enter`
API: GET /api/v1/github
MCP: get_github
```

The CLI, the TUI, the API and MCP give you the link; installing always happens on github.com. GitHub sends you back and the repos show up. The same command shows where the app is installed.

The app asks for the least it needs: read the contents of the repos you choose, and write commit statuses. It cannot push code.

## See the repos

Lists the repos ops may read.

```surfaces
Web: the app page, **GitHub**
CLI: ops github repos
TUI: on the app, `g`
API: GET /api/v1/github/repos
MCP: list_repos
```

In the TUI, `g` on an app opens the GitHub panel: the repo the app follows and the repos ops may read.

## Link a repo

Makes an app follow a repo and a branch. Deploy every push is on by default.

```surfaces
Web: the app page, **GitHub**, choose the repo, **Link repo**
CLI: ops link me/blog --branch main
TUI: on the app, `g`, then type `link me/blog main`
API: PUT /api/v1/apps/{app}/repo
MCP: link_repo
```

## Deploy from GitHub

Deploys the linked branch, or any branch, tag or commit, without a checkout on your computer.

```surfaces
Web: the app page, **GitHub**, type a branch, tag or commit or leave it empty, **Deploy**
CLI: ops deploy --ref v1.4.0
TUI: on the app, `g`, type a branch, tag or commit or nothing, then `enter`
API: POST /api/v1/apps/{app}/deploys
MCP: deploy_ref
```

Empty means the linked branch. In the API, send `{"ref": "v1.4.0"}`, or `{"ref": ""}` for the linked branch.

## Deploy every push

Deploys each push to the linked branch by itself.

```surfaces
Web: the app page, **GitHub**, **Deploy every push**
CLI: ops link me/blog --auto=false
TUI: on the app, `g`, then type `link me/blog main manual`
API: PUT /api/v1/apps/{app}/repo
MCP: link_repo
```

These turn it off; `ops link me/blog` and `link me/blog` in the TUI turn it on again. In the API and MCP it is `auto_deploy`.

A push to another branch does nothing. A push that arrives while a deploy of the app still runs is skipped; the next push, or **Deploy**, picks up the newest commit.

## Unlink

Stops the app from following the repo. The app keeps running.

```surfaces
Web: the app page, **GitHub**, **Unlink**
CLI: ops unlink
TUI: on the app, `g`, then type `unlink`
API: DELETE /api/v1/apps/{app}/repo
MCP: unlink_repo
```

## How a deploy from GitHub runs

1. ops resolves the ref to a commit: `main` becomes `5ee5ee5`.
2. Your server downloads that commit straight from GitHub, with a short-lived token the control plane gets for it. The code never passes through ops.
3. Your server builds and starts it the same way, with a snapshot of the database first.
4. The commit gets the status **pending**, then **success** or **failure** with a link to the app.

The deploy list shows the commit and its message, in the web, the TUI and the API.

## Without the GitHub App

`ops deploy` uploads the directory you are in, and [CI](/docs/guides/ci) can build an image and call a deploy hook. Both work without connecting GitHub.
