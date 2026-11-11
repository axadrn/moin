---
title: "How moin ships"
description: "From a change to your server: pull requests, deploys, releases, versions and updates"
group: "Reference"
order: 6
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

## What runs where

- **moin.run** runs the last deploy or release.
- **Your own hub** runs releases only. A new one shows up in the web UI, and one click updates. [Self-hosting](/docs/self-hosting#updates)
- **Daemons** run releases only. They update themselves, one server at a time, and your apps keep running. [Servers](/docs/guides/servers#updates)

## Images

The hub is one image, `ghcr.io/axadrn/moin`.

| Tag | Is |
| --- | --- |
| `stable`, `latest` | the newest release. Your hub runs `stable`. |
| `v1.0.0-beta.28` | exactly that version, forever |
| `main-c00bd77` | one deploy of moin.run. Not for your hub. |

## Your version

- Web: **Settings**, **Version**
- CLI: `moin version`
