---
title: "Build in CI"
description: "Build images in GitHub Actions and deploy them with a hook"
order: 13
---

By default your server builds the app. You can also build the image in CI, push it to a registry and let ops deploy it. The server then only pulls and starts it.

| | Server builds | CI builds |
| --- | --- | --- |
| Who builds | your server, from the code `ops deploy` uploads | GitHub Actions or any CI |
| Start it | `ops deploy` | the deploy hook, or `ops deploy --image` (after one `ops deploy` of the repo, whose compose file it reuses) |
| Good for | small apps, no registry needed | heavy builds, build caches, many servers |

An image deploy keeps the compose file of the running release and only swaps the image of the main service. So deploy the repo once with `ops deploy`; after that CI can ship images. Health check, automatic rollback, `ops rollback` and env vars work the same.

## 1. Save a registry login

Lets your servers pull private images. A registry is where CI stores the images it builds, such as GitHub Container Registry. Public images need no login.

```surfaces
Web: **Settings**, **Tokens** tab, **Registries**, **Save login**
CLI: ops registry set ghcr.io <user>
TUI: settings tab, `a`
API: PUT /api/v1/registries/{host}
MCP: set_registry
```

For GitHub Container Registry, create a token with `read:packages`. The CLI asks for it on stdin. The token is stored encrypted. The agent gets it only for the pull, in a throwaway Docker config; nothing stays on the server.

### See the registry logins

Lists the saved logins, without their passwords.

```surfaces
Web: **Settings**, **Tokens** tab, **Registries**
CLI: ops registry
TUI: settings tab
API: GET /api/v1/registries
MCP: list_registries
```

### Remove a registry login

Deletes a saved login. Servers can then no longer pull private images from that registry.

```surfaces
Web: **Settings**, **Tokens** tab, **Registries**, **Remove** on the login
CLI: ops registry rm ghcr.io
TUI: settings tab, pick the login, `x`, then `y`
API: DELETE /api/v1/registries/{host}
MCP: remove_registry
```

## 2. Create a deploy hook

Gives you a secret URL that deploys an image when CI calls it.

```surfaces
Web: the app page, **Deploy hook**, **New deploy hook**
CLI: ops hook blog
TUI: on the app, `h`
API: POST /api/v1/apps/{app}/hook
MCP: create_deploy_hook
```

ops shows the URL once. Store it as the CI secret `OPS_DEPLOY_HOOK`. A new hook makes the old one stop working, so ops asks first. In a script, add `--yes`.

### Turn the deploy hook off

Makes the hook URL stop working, so CI can no longer deploy. Do it when you stop using CI or think the URL leaked. A new hook can be made again any time.

```surfaces
Web: the app page, **Deploy hook**, **Turn off**
CLI: ops hook off blog
TUI: on the app, `H`, then `y`
API: DELETE /api/v1/apps/{app}/hook
MCP: remove_deploy_hook
```

It works also when the plan allows no changes. `get_app` and `GET /api/v1/apps/{app}` say in `hook` whether the app has one.

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
            "$OPS_DEPLOY_HOOK?image=ghcr.io/${{ github.repository }}:${{ github.sha }}"
        env:
          OPS_DEPLOY_HOOK: ${{ secrets.OPS_DEPLOY_HOOK }}
```

Build for the arch of your server (`ops servers` shows it). The hook answers `202` with the deploy; follow it in the web UI, the TUI or with `ops logs`.

## Or deploy with the ops action

The ops GitHub Action installs `ops` from your control plane and runs `ops deploy` with a personal API token. Without `image` it puts the repo into [your bucket](/docs/guides/bucket) and your server builds it; with `image` it deploys the image the job pushed.

```yaml
      - uses: axadrn/ops@main
        with:
          url: https://ops.example.com
          token: ${{ secrets.OPS_TOKEN }}
          image: ghcr.io/${{ github.repository }}:${{ github.sha }}
```

The step waits until the deploy is live and fails the job when the deploy fails. For deploys without CI, [GitHub](/docs/guides/github) deploys every push by itself.

## Deploy an image by hand

Deploys an image that is already in a registry, without CI.

```surfaces
Web: the app page, **Deploy an image**, **Deploy image**
CLI: ops deploy --image ghcr.io/me/blog:3f2a9c1 blog
TUI: on the app, `i`
API: POST /api/v1/apps/{app}/deploys
MCP: deploy_image
```

In the API, send `{"image": "ghcr.io/me/blog:3f2a9c1"}`. A redeploy of an image deploy pulls the same image again.
