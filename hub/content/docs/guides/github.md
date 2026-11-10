---
title: "GitHub"
description: "Deploy any branch, tag or commit without a checkout. And every push."
group: "Apps"
order: 4
---

Connect GitHub. moin takes your code straight from it. Link a repo to an app once. Then deploy any branch, tag or commit. Every push too, if you like. When the deploy is live, the pull request shows a green check: **moin/deploy**.

## Connect GitHub

Installs the moin GitHub App on your account or an organization. Once per workspace.

```surfaces
Web: **Settings**, **GitHub**, **Connect GitHub**; or the app's **Deploys** tab, **GitHub**, **Connect GitHub**
CLI: moin github
TUI: opens the web: `w`, then `g`
API: GET /api/v1/github
MCP: get_github
```

Connecting always happens on github.com. The CLI, the API and MCP give you the link. The app may only read the repos you choose and write commit statuses. It cannot push code.

## Link a repo

```surfaces
Web: the app's **Deploys** tab, **GitHub**, choose the repo and the branch, **Deploy**
CLI: moin link <owner>/<repo> --branch main
TUI: on the app, `W` opens its GitHub card in the web
API: PUT /api/v1/apps/{app}/repo
MCP: link_repo
```

Deploy every push is on by default. On the web, **Deploy** links and deploys in one step. Elsewhere, link first and then deploy.

## Deploy from GitHub

```surfaces
Web: the app's **Deploys** tab, **GitHub**, type a branch, tag or commit or leave it empty, **Deploy**
CLI: moin deploy --ref <ref>
TUI: on the app, `B`, type a branch, tag or commit or nothing, then `enter`
API: POST /api/v1/apps/{app}/deploys
MCP: deploy_ref
```

```bash
moin deploy --ref main      # a branch
moin deploy --ref v1.4.0    # a tag
moin deploy --ref 3f2a9c1   # a commit
```

Empty means the linked branch. The API takes `{"ref": "v1.4.0"}`.

Your server downloads the commit straight from GitHub, with a short-lived token. The code never passes through moin.

## Deploy every push

```text
git push    to the branch the app
   │        follows
   ▼
GitHub      tells moin
   │
   ▼
moin        tells the daemon
   │
   ▼
daemon      downloads the commit
   │        and deploys it
   ▼
live ✓      the commit gets a green
            check: moin/deploy
```

```surfaces
Web: the app's **Deploys** tab, **GitHub**, **Deploy every push**
CLI: moin link <owner>/<repo> --auto=false
TUI: on the app, `W` opens its GitHub card in the web
API: PUT /api/v1/apps/{app}/repo
MCP: link_repo
```

`--auto=false` turns it off. `moin link <owner>/<repo>` turns it on again. The API and MCP call it `auto_deploy`. A push while a deploy runs is skipped. The next push, or **Deploy**, takes the newest commit.

## See the repos and branches

```surfaces
Web: the app, **GitHub**; the **Branch** select after you choose the repo
CLI: moin github repos
CLI: moin github branches <owner>/<repo>
TUI: on the app, `W` opens its GitHub card in the web
API: GET /api/v1/github/repos
API: GET /api/v1/github/repos/{owner}/{repo}/branches
MCP: list_repos
MCP: list_branches
```

Your repo is missing? Every surface links to the page on github.com where you choose the repos moin may read.

## Unlink

The app stops following the repo and keeps running.

```surfaces
Web: the app's **Deploys** tab, **GitHub**, **Unlink**
CLI: moin unlink
TUI: on the app, `W` opens its GitHub card in the web
API: DELETE /api/v1/apps/{app}/repo
MCP: unlink_repo
```

## Good to know

- A branch or ref name follows git's rules and has at most 255 characters.
- Already installed the app? Press **Connect GitHub** anyway. moin asks GitHub which accounts have it.
- A self-hosted hub needs [your own GitHub App](/docs/self-hosting#your-own-github-app).
