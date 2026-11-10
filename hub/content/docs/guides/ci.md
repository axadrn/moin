---
title: "Build in CI"
description: "Build images in GitHub Actions or any CI. Deploy them with a hook."
group: "Apps"
order: 5
---

CI is a computer elsewhere that builds your app. GitHub Actions, for example. It packs the app into an image, a box ready to run. It puts the image on a registry, a shelf for images. Your server only downloads and starts it.

Deploy the repo once with `moin deploy` first. An image deploy keeps that compose file and swaps only the image of the main service.

```text
git push
   │
   ▼
CI         builds the image and
   │       puts it on a registry
   │
   │ calls the deploy hook
   ▼
moin       tells the daemon
   │
   ▼
daemon     pulls the image from
   │       the registry
   ▼
live ✓     started and checked
```

The deploy hook is a secret URL. When CI calls it, moin deploys the image.

## 1. Save a registry login

Only for private images. For GitHub Container Registry, use a token with `read:packages`.

```surfaces
Web: **Settings**, **Account**, **Tokens** tab, **Registries**, **Save login**
CLI: moin registry set ghcr.io <username>
TUI: opens the web: `w`, then `s`
API: PUT /api/v1/registries/{host}
MCP: set_registry
```

The CLI reads the token from stdin. It is stored encrypted. The daemon gets it only for the pull.

## 2. Create a deploy hook

moin shows the URL once. Store it as the CI secret `MOIN_DEPLOY_HOOK`.

```surfaces
Web: the app's **Deploys** tab, **Deploy from CI**, **New deploy hook**
CLI: moin hook <app-name>
TUI: on the app, `W` opens **Deploy from CI** in the web
API: POST /api/v1/apps/{app}/hook
MCP: create_deploy_hook
```

A new hook stops the old one, so moin asks first when there is one. `--yes` skips it.

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
            "$MOIN_DEPLOY_HOOK?image=ghcr.io/${{ github.repository }}:${{ github.sha }}"
        env:
          MOIN_DEPLOY_HOOK: ${{ secrets.MOIN_DEPLOY_HOOK }}
```

Build for the arch of your server. `moin servers` shows it. The hook answers `202` with the deploy.

## Or use the moin action

The action installs `moin` from your hub and runs `moin deploy --yes`. It needs an [API token](/docs/guides/account#create-an-api-token) with **Read and write**, saved in the repo as `MOIN_TOKEN`.

With `image` it deploys that image. Without it, the repo goes through [your bucket](/docs/guides/bucket) and your server builds it. The job fails when the deploy fails.

```yaml
      - uses: axadrn/moin@main
        with:
          url: https://moin.example.com
          token: ${{ secrets.MOIN_TOKEN }}
          image: ghcr.io/${{ github.repository }}:${{ github.sha }}
```

## Deploy an image by hand

```surfaces
Web: the app's **Deploys** tab, **Deploy from CI**, **Deploy image**
CLI: moin deploy --image ghcr.io/<owner>/<image>:<tag> <app-name>
TUI: on the app, `I`, type the image, then `enter`
API: POST /api/v1/apps/{app}/deploys
MCP: deploy_image
```

The API takes `{"image": "ghcr.io/<owner>/<image>:<tag>"}`.

## Turn the deploy hook off

The hook URL stops working, so CI can no longer deploy. Do it when you stop using CI or the URL leaked. You can make a new hook any time.

```surfaces
Web: the app, **Deploy from CI**, **Deploy hook**, **Turn off**
CLI: moin hook off <app-name>
TUI: on the app, `W` opens **Deploy from CI** in the web
API: DELETE /api/v1/apps/{app}/hook
MCP: remove_deploy_hook
```

## Registry logins

```surfaces
Web: **Settings**, **Account**, **Tokens** tab, **Registries**; **Remove** on a login
CLI: moin registry
CLI: moin registry rm ghcr.io
TUI: opens the web: `w`, then `s`
API: GET /api/v1/registries
API: DELETE /api/v1/registries/{host}
MCP: list_registries
MCP: remove_registry
```

The list never shows passwords. Public images need no login.

## Good to know

- The fallback to the last good deploy, `moin rollback` and env vars work the same as for a server build.
