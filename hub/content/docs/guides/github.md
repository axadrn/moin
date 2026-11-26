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
Web: **Settings**, **Integrations**, **Connect GitHub**; or the app's **Deploys** tab, **Deploy from**, **GitHub**, **Connect GitHub**
CLI: moin github
TUI: opens the web: `w`, then `g`
API: GET /api/v1/github
MCP: get_github
```

Connecting always happens on github.com. The CLI, the API and MCP give you the link. The app may only read the repos you choose and write commit statuses. It cannot push code.

## Link a repo

```surfaces
Web: the app's **Deploys** tab, **Deploy from**, **GitHub**, pick the repo and the branch, **Link and deploy**
CLI: moin link <owner>/<repo> --branch main
TUI: on the app, `W` opens it in the web, then **Deploys**
API: PUT /api/v1/apps/{app}/repo
MCP: link_repo
```

Deploy every push is on by default, on every surface. The API and MCP turn it off with `auto_deploy: false`. On the web, **Link and deploy** links and deploys in one step. Elsewhere, link first and then deploy. Or run [`moin deploy`](/docs/guides/deploy#deploy-what-you-pushed) in the pushed repo: it links the app and deploys, without deploys on push.

An app deploys from one place at a time: [GitHub or CI](/docs/guides/deploy#deploy-from). Deploy every push turns the deploy hook off, so moin asks first. `--yes` skips it.

An app with a deploy hook links without deploy on push on the web, and in the API and MCP when `auto_deploy` is left out. The CLI asks first, or add `--auto=false`. Then CI keeps deploying, and each image deploy takes `compose.yml` from the repo: [Build in CI](/docs/guides/ci).

## Deploy from GitHub

```surfaces
Web: the app's **Deploys** tab, **Deploy from**, **GitHub**, type a branch, tag or commit or leave it empty, **Deploy**
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

Empty means the linked branch. The API takes `{"ref": "v1.4.0"}`. A commit named by its SHA, here or from CI, must be on the linked branch: GitHub also serves a fork's commits through the repo they forked, and moin never deploys one of those.

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
Web: the app's **Deploys** tab, **Deploy from**, **GitHub**, **Deploy every push**
CLI: moin link <owner>/<repo> --auto=false
TUI: on the app, `W` opens it in the web, then **Deploys**
API: PUT /api/v1/apps/{app}/repo
MCP: link_repo
```

`--auto=false` turns it off. `moin link <owner>/<repo>` turns it on again. The API and MCP call it `auto_deploy`. A push while a deploy runs is skipped. The next push, or **Deploy**, takes the newest commit.

Turning it on turns the [deploy hook](/docs/guides/ci) off. Turning the hook on turns it off. While the hook is on, the web shows no switch: turn the hook off first.

## See the repos and branches

```surfaces
Web: the app's **Deploys** tab, **Deploy from**, **GitHub**: the **Repo** select, then the **Branch** select
CLI: moin github repos
CLI: moin github branches <owner>/<repo>
TUI: on the app, `W` opens it in the web, then **Deploys**
API: GET /api/v1/github/repos
API: GET /api/v1/github/repos/{owner}/{repo}/branches
MCP: list_repos
MCP: list_branches
```

Your repo is missing? Every surface links to the page on github.com where you choose the repos moin may read.

## Unlink

The app stops following the repo and keeps running.

```surfaces
Web: the app's **Deploys** tab, **Deploy from**, **GitHub**, **Unlink**
CLI: moin unlink
TUI: on the app, `W` opens it in the web, then **Deploys**
API: DELETE /api/v1/apps/{app}/repo
MCP: unlink_repo
```

## Good to know

- A branch or ref name follows git's rules and has at most 255 characters.
- Already installed the app? Press **Connect GitHub** anyway. moin asks GitHub which accounts have it.
- A self-hosted hub needs [your own GitHub App](/docs/self-hosting#your-own-github-app).
