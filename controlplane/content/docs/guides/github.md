---
title: "GitHub"
description: "Deploy a branch or commit without a checkout, and every push"
order: 9
---

Connect GitHub once, link a repo to an app, and ops deploys from GitHub: any branch, tag or commit from the web, the CLI, the terminal UI or an agent, with no checkout on your machine. With auto deploy every push to the branch deploys. ops writes how it went back to GitHub as the commit status **ops/deploy**, so the pull request shows a green check when the deploy is live.

| Action | Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- | --- |
| Connect GitHub | app page, **Connect GitHub** | `ops github` prints the link | settings tab, **GitHub**, `enter` shows the link | `GET /api/v1/github` | `get_github` |
| See where it is installed | **Repository** | `ops github` | settings tab, **GitHub** | `GET /api/v1/github` | `get_github` |
| List repos | **Repository** | `ops github repos` | `g` | `GET /api/v1/github/repos` | `list_repos` |
| Link a repo | **Repository**, choose it, **Link** | `ops link me/blog [--branch main]` | `g`, then `link me/blog [main]` | `PUT /api/v1/apps/{app}/repo` | `link_repo` |
| Deploy the linked branch | **Deploy** | `ops deploy --ref main` | `g`, then `enter` | `POST /api/v1/apps/{app}/deploys` with `{"ref": ""}` | `deploy_ref` |
| Deploy a branch, tag or commit | type it, **Deploy** | `ops deploy --ref v1.4.0` | `g`, then type it, `enter` | `POST /api/v1/apps/{app}/deploys` with `{"ref": "v1.4.0"}` | `deploy_ref` with `ref` |
| Deploy every push | **Deploy every push** | `ops link me/blog` (on by default), `--auto=false` turns it off | `g`, then `link me/blog` (on), `link me/blog main manual` (off) | `PUT /api/v1/apps/{app}/repo` with `auto_deploy` | `link_repo` with `auto_deploy` |
| Unlink | **Unlink** | `ops unlink` | `g`, then `unlink` | `DELETE /api/v1/apps/{app}/repo` | `unlink_repo` |

In the terminal UI, `g` on an app opens the GitHub panel: the repo the app follows and the repos ops may read. `enter` on its own deploys the linked branch; type a branch, tag or commit to deploy that instead.

## Connect

**Connect GitHub** opens GitHub, where you install the ops GitHub App on your account or an organization and choose its repos. GitHub sends you back and the repos show up. Installing happens on github.com in every case; `ops github` prints the link for the terminal.

The app asks for the least it needs: read the contents of the repos you choose, and write commit statuses. It cannot push code.

## How a deploy from GitHub runs

1. ops resolves the ref to a commit: `main` becomes `5ee5ee5`.
2. The control plane downloads that commit as a tarball from GitHub and stores it like an `ops deploy` upload.
3. Your server builds and starts it the same way, with a snapshot of the database first.
4. The commit gets the status **pending**, then **success** or **failure** with a link to the app.

The deploy list shows the commit and its message, in the web, the TUI and the API.

## Auto deploy

With auto deploy on, a push to the linked branch deploys that commit. Pushes to other branches do nothing. A push that arrives while a deploy of the app still runs is skipped; the next push, or **Deploy**, picks up the newest commit.

## Without the GitHub App

`ops deploy` uploads the directory you are in, and [CI](/docs/guides/ci) can build an image and call a deploy hook. Both work without connecting GitHub.
