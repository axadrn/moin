---
title: "Build in CI"
description: "Build images in GitHub Actions or any CI. Deploy them with a hook."
group: "Apps"
order: 5
---

CI is a computer elsewhere that builds your app. GitHub Actions, for example. It packs the app into an image, a box ready to run. It puts the image on a registry, a shelf for images. Your server only downloads and starts it.

The image is the code. Your repo still has the rest: `compose.yml` with the databases and volumes. Each deploy takes `compose.yml` from the commit CI built the image from. Code and compose file always match.

1. [Link the repo](/docs/guides/github#link-a-repo). The hook turns deploy on push off later.
2. [Deploy your image once by hand](#deploy-an-image-by-hand). That picks the image.
3. [Create a deploy hook](#2-create-a-deploy-hook). From then on CI calls it with each new tag and its commit.

An app deploys from one place at a time: [GitHub or CI](/docs/guides/deploy#deploy-from). Turning the hook on turns deploy on push off. The repo stays linked, for its `compose.yml`.

```text
git push
   │
   ▼
CI         builds the image and
   │       puts it on a registry
   │
   │ calls the deploy hook
   │ with image and commit
   ▼
moin       tells the daemon
   │
   ▼
daemon     downloads the commit from
   │       GitHub for its compose.yml,
   │       pulls the image from
   │       the registry, builds nothing
   ▼
live ✓     started and checked
```

The deploy hook is a secret URL. When CI calls it, moin deploys the image. Only a new tag of the image running now. Nothing else.

## 1. Save a registry login

Only for private images. For GitHub Container Registry, use a token with `read:packages`.

```surfaces
Web: **Settings**, **Registries**, **Save login**
CLI: moin registry set ghcr.io <username> --server web-1
TUI: opens the web: `w`, then `r`
API: PUT /api/v1/registries/{host}
MCP: set_registry
```

The CLI reads the token from stdin. The login goes to the server you pick, which keeps it. The hub keeps only the host and the username. The server must be online. With one server, moin picks it.

Each server that pulls the image needs the login. A server without it is listed, and an image deploy there stops with `web-2 has no login for ghcr.io`. Save the login again for that server.

## 2. Create a deploy hook

moin shows the URL once. Store it as a CI secret, for example `MOIN_DEPLOY_HOOK`. Keep it secret, like a password.

```surfaces
Web: the app's **Deploys** tab, **Deploy from**, **CI**, **Turn on**
CLI: moin hook <app-name>
TUI: on the app, `W` opens its Deploys tab in the web
API: POST /api/v1/apps/{app}/hook
MCP: create_deploy_hook
```

A new hook stops the old one and turns deploy on push off, so moin asks first when that turns something off. `--yes` skips it. The repo stays linked.

### One image, new tags

A secret URL can leak, in a log or a screenshot. So a hook deploys only new tags of the image running now: `ghcr.io/me/app:3f2a9c1` after `ghcr.io/me/app:v1`. Any registry works, Docker Hub too.

- Another image gets `422`: "This deploy hook deploys only new tags of ghcr.io/me/app, the image running now."
- While the app runs a build of its repo, the hook deploys nothing. Deploy an image by hand once.
- To change the image, deploy the new one by hand once. The hook follows. Its URL stays.
- **Deploy from**, **CI** and `moin hook` show the image. The API and MCP call it `hook_image`.

## 3. The workflow

```yaml
name: deploy
on:
  push:
    branches: [main]
permissions:
  contents: read
  packages: write
jobs:
  deploy:
    runs-on: ubuntu-24.04-arm
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/build-push-action@v6
        with:
          push: true
          platforms: linux/arm64
          tags: ghcr.io/${{ github.repository }}:${{ github.sha }}
      - run: |
          curl -fsS -X POST \
            "$MOIN_DEPLOY_HOOK?image=ghcr.io/${{ github.repository }}:${{ github.sha }}&commit=${{ github.sha }}"
        env:
          MOIN_DEPLOY_HOOK: ${{ secrets.MOIN_DEPLOY_HOOK }}
```

Build for the arch of your server. `moin servers` shows it. The hook answers `202` with the deploy.

The hook takes `?image=` and `&commit=`, or the JSON body `{"image": "...", "commit": "..."}`. The commit is the one the image was built from, a full or short SHA. moin takes its `compose.yml` from GitHub. Without a commit, the head of the linked branch. An app that follows no repo keeps the `compose.yml` it runs now, and a commit is refused.

## Or use the moin action

The action installs `moin` from your hub and runs `moin deploy --yes`. It needs a [CI token](/docs/guides/account#make-a-ci-token) with the deploy right, saved in the repo as `MOIN_TOKEN`. Limit it to the app it deploys, and name that app in `app:`: a token for one app cannot list the others to find it.

With `image` it deploys that image, with the `compose.yml` of the job's commit. Set `commit: ''` for an app that follows no repo. Without `image`, your server builds the commit of the job from GitHub, as [`moin deploy`](/docs/guides/deploy#deploy-what-you-pushed) does. The job fails when the deploy fails.

```yaml
      - uses: axadrn/moin@main
        with:
          url: https://moin.example.com
          token: ${{ secrets.MOIN_TOKEN }}
          image: ghcr.io/${{ github.repository }}:${{ github.sha }}
```

## Deploy an image by hand

```surfaces
Web: the app's **Deploys** tab, **Deploy from**, **CI**, **1 Image**, **Deploy**
CLI: moin deploy --image ghcr.io/<owner>/<image>:<tag> [--ref <commit>] <app-name>
TUI: on the app, `I`, type the image, then `enter`
API: POST /api/v1/apps/{app}/deploys
MCP: deploy_image
```

The API takes `{"image": "ghcr.io/<owner>/<image>:<tag>", "ref": "<commit>"}`. The `compose.yml` comes from the ref in the linked repo, a branch, tag or commit. Without one, the head of the linked branch. The web and the TUI always take the branch.

## Turn the deploy hook off

The hook URL stops working, so CI can no longer deploy. Do it when you stop using CI or the URL leaked. You can make a new hook any time.

The repo stays linked. Pushes still do not deploy until you turn **Deploy every push** on.

```surfaces
Web: the app's **Deploys** tab, **Deploy from**, **CI**, **Turn off**
CLI: moin hook off <app-name>
TUI: on the app, `W` opens its Deploys tab in the web
API: DELETE /api/v1/apps/{app}/hook
MCP: remove_deploy_hook
```

## Registry logins

```surfaces
Web: **Settings**, **Registries**; **Remove** on a login
CLI: moin registry
CLI: moin registry rm ghcr.io
TUI: opens the web: `w`, then `r`
API: GET /api/v1/registries
API: DELETE /api/v1/registries/{host}
MCP: list_registries
MCP: remove_registry
```

The list never shows passwords. It says which servers keep each login and which do not. Removing a login removes it from the servers too. Public images need no login.

## Good to know

- The fallback to the last good deploy, `moin rollback` and env vars work the same as for a server build.
