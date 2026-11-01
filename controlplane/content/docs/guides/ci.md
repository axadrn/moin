---
title: "Build in CI"
description: "Build images in GitHub Actions and deploy them with a hook"
order: 13
---

By default your server builds the app. You can also build the image in CI, push it to a registry and let ops deploy it. The server then only pulls and starts it.

| | Server builds | CI builds |
| --- | --- | --- |
| Who builds | your server, from the source `ops deploy` uploads | GitHub Actions or any CI |
| Start it | `ops deploy` | the deploy hook, or `ops deploy --image` (after one `ops deploy` of the repo, whose compose file it reuses) |
| Good for | small apps, no registry needed | heavy builds, build caches, many servers |

An image deploy keeps the compose file of the running release and only swaps the image of the main service. So deploy the repo once with `ops deploy`; after that CI can ship images. Health check, automatic rollback, `ops rollback` and env vars work the same.

## 1. A login for private images

Public images pull without one. For GitHub Container Registry, create a token with `read:packages`:

| Action | Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- | --- |
| List logins | **Settings → Tokens**, Registries | `ops registry` | settings tab | `GET /api/v1/registries` | `list_registries` |
| Save a login | **Settings → Tokens**, Registries | `ops registry set ghcr.io <user>`, then the token | settings tab, `a` | `PUT /api/v1/registries/{host}` | `set_registry` |
| Remove a login | **Remove** on the login | `ops registry rm ghcr.io` | settings tab, `x`, `y` | `DELETE /api/v1/registries/{host}` | `remove_registry` |

The token is stored encrypted. The agent gets it only for the pull, in a throwaway Docker config; nothing stays on the server.

## 2. A deploy hook

| Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- |
| app page, **Deploy hook** | `ops hook [app]` | `h` | `POST /api/v1/apps/{app}/hook` | `create_deploy_hook` |

It prints a secret URL once. Store it as the CI secret `OPS_DEPLOY_HOOK`. Creating a new one makes the old one stop working.

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
      - run: curl -fsS -X POST "$OPS_DEPLOY_HOOK?image=ghcr.io/${{ github.repository }}:${{ github.sha }}"
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

| Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- |
| app page, **Deploy image** | `ops deploy --image ghcr.io/me/blog:3f2a9c1 [app]` | `i` | `POST /api/v1/apps/{app}/deploys` with `{"image": …}` | `deploy_image` |

A redeploy of an image deploy pulls the same image again.
