---
title: "How moin ships"
description: "From a change to your server: pull requests, deploys, releases, versions and updates"
group: "Reference"
order: 7
---

This page is the one place that says how a change gets from the code to your server. When that changes, this page changes in the same pull request.

```text
change → pull request → main → deploy  → moin.run
                             → release → moin.run, every self-hosted hub, every daemon
```

## A change

One branch, one small thing. Everyone, maintainers too:

1. A pull request. Nobody pushes to `main`.
2. CI turns green.
3. A maintainer reviews it.
4. A squash merge: one commit on `main`.

Merging ships nothing. The change only lands on `main`. [Contributing](https://github.com/axadrn/moin/blob/main/CONTRIBUTING.md)

## Deploy or release

| What changed | What ships it | Who gets it |
| --- | --- | --- |
| Only the repository: `AGENTS.md`, tests, workflows | nothing, the merge is enough | nobody |
| What you see, not what moin does: docs, text, design | a **deploy** | moin.run |
| What moin or a daemon does: a feature or a fix | a **release** | everyone |

A release has a version, like `v1.0.0-beta.28`, and its notes in the [changelog](/changelog). A deploy has neither.

A release also carries the `moin` binaries, their `SHA256SUMS` and its signature, `SHA256SUMS.sig`. `SHA256SUMS` also names the hub image of the release by its digest, so the signature covers the image too. The Deploy workflow signs with `RELEASE_SIGNING_KEY`, a secret of the GitHub environment `release`: only a `v*` tag reaches it, and only after a maintainer approves the run. It never reaches a hub. Daemons update from there. [Updates](/docs/guides/security#updates)

A release waits for a maintainer to approve it in GitHub. Then the workflow signs, makes the GitHub release as a draft, adds every file, and only then publishes it and tags its image with its version and `stable`. Before that, the image is only `candidate-<commit>`. No hub sees a new version before its signed release is complete.

## What runs where

- **moin.run** runs the last deploy or release.
- **Your own hub** runs releases only. A new one shows up in the web UI. `sh /opt/moin/update.sh` installs it. [Self-hosting](/docs/self-hosting#updates)
- **Every hub update**, moin.run's too, first starts the new hub once, on a copy of the database, with its real settings. If that fails, the update stops and the running hub stays. [How a hub updates](#how-a-hub-updates)
- **Daemons** run releases only. They update themselves, one server at a time, and your apps keep running. [How a daemon updates](#how-a-daemon-updates)

## How a hub updates

Every hub updates the same way: `update.sh` in `/opt/moin`, with its settings in `/opt/moin/.env`.

1. `docker compose pull` gets the new version. The running hub keeps serving.
2. `./main preflight` starts the new hub once, on a copy of the database, with its settings. It runs the migrations on the copy and opens what the database keeps, such as the GitHub App with `SECRETS_KEY`. Then it removes the copy. No visitor reaches it. If it fails, `update.sh` stops. The running hub stays.
3. `docker compose up -d --wait` switches to the new version and returns once it is healthy.

moin.run gets one more step, from the Deploy workflow, through Tailscale:

1. It copies the deploy files of the commit to `/opt/moin`.
2. It fills the settings from 1Password: `op inject` turns `/opt/moin/.env.tpl`, with `op://` references, into `/opt/moin/.env`. The token of a service account is in `/etc/moin-hub/op-token`. If that fails, the deploy stops. The running hub stays.
3. It runs `update.sh` with the image of the deploy or release, `MOIN_IMAGE=ghcr.io/axadrn/moin:<tag>`.
4. It waits until the hub answers on `/healthz`.

In `.env.tpl`, a value that can hold a `$` goes in single quotes, like `S3_SECRET_KEY='{{ op://vault/item/field }}'`. Compose would read `$` as a variable. A new setting goes into the template, never into `.env` by hand: the next deploy writes `.env` fresh.

### Once, before the first deploy with `/opt/moin/.env`

Up to v1.0.0-beta.36 moin.run kept its template in `/etc/moin-hub/env.tpl`. Before the first deploy that brings `/opt/moin/.env`, as root on the server:

1. `install -m 600 /etc/moin-hub/env.tpl /opt/moin/.env.tpl`
2. In `/opt/moin/.env.tpl`: add `COMPOSE_FILE=compose.yml:compose.tunnel.yml` and `DOMAIN=` with the host of `APP_URL`. Delete `APP_URL`, and `SAME_SERVER` if it is there. Put every `op://` value that may hold a `$` in single quotes.
3. Check it fills: `cd /opt/moin && OP_SERVICE_ACCOUNT_TOKEN=$(cat /etc/moin-hub/op-token) docker run --rm -i -e OP_SERVICE_ACCOUNT_TOKEN 1password/op:2 op inject < .env.tpl > /dev/null`
4. Run the deploy. Once it is green: `rm -rf /etc/moin-hub/env /etc/moin-hub/env.tpl /opt/moin/updater.sh /var/lib/moin-hub/update`

Without step 1 the deploy stops at `op inject`, and the running hub stays. Other templates in `/etc/moin-hub`, such as one for a key file, are no longer filled. The files they made stay as they are.

## How a daemon updates

A daemon always runs the version of its hub. When the hub gets a new release:

1. The hub tells the daemon: update to this version.
2. The daemon downloads the new `moin` from the GitHub release and checks our signature. [Updates](/docs/guides/security#updates)
3. It swaps itself and restarts. Your apps keep running, unless the update needs them stopped (below).
4. It calls the hub again, now on the new version.

- One server at a time, never during a deploy. A server that is busy gets its turn later.
- If a daemon does not come back, moin stops there and says so in the activity.
- A server with [rules](/docs/guides/security#rules) updates like any other: the release carries our signature, so a new daemon cannot drop its rules.
- Turn the automatic updates off, or update one server by hand: [Servers](/docs/guides/servers#updates).

**Some updates need a step first**, like stopping the apps that use SQLite while the old daemon exits. moin does it for you: it stops them, updates the daemon and starts them again. You never do a step by hand.

- If a step fails before the update, moin starts the apps again, so all is as before, and alerts you (`server.daemon_update_failed`). It does not try again by itself.
- If the hub or the server stops halfway, moin finishes the update when the daemon is back.
- A server several versions behind gets all their steps at once: everything that goes before, one update, everything that goes after.

These steps are one list in `hub/internal/service/daemon_steps.go`. It only grows.

The hub only sends the order. The code that does it is in the daemon. [What the daemon does](/docs/concepts/how-moin-works#what-the-daemon-does)

## The CLI and the TUI

The hub refuses a CLI or TUI older than `MinClientVersion` in `hub/internal/middleware/client_version.go`. It answers `426` `client_outdated` with the command that updates it. A release that breaks older ones raises it to its own version.

## Images

The hub is one image, `ghcr.io/axadrn/moin`.

| Tag | Is |
| --- | --- |
| `stable`, `latest` | the newest release, once its signed release is complete. Your hub runs `stable`. |
| `v1.0.0-beta.28` | exactly that version, forever, once its signed release is complete |
| `candidate-c00bd77` | a release waiting for its approval. Not for your hub. |
| `main-c00bd77` | one deploy of moin.run. Not for your hub. |

A release's signed `SHA256SUMS` names its image by digest: the line `<digest>  ghcr.io/axadrn/moin:v1.2.3`. `stable` is that same image. Pull it by the digest to run exactly what we signed: [Updates](/docs/guides/security#updates)

## Your version

- Web: **Settings**, **Version**
- CLI: `moin version`
