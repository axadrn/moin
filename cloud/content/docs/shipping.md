---
title: "How moin ships"
description: "From a change to your server. Pull requests. Deploys of the site. Signed releases. Updates you run yourself."
group: "Reference"
order: 4
---

This page is the one place that says how a change gets from the code to your server. When that changes, this page changes in the same pull request.

```text
change → pull request → main → deploy  → moin.run, the site
                             → release → GitHub, signed → moin update, when you run it
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
| The site: docs, blog, landing | a **deploy** | moin.run |
| What `moin` does: a feature or a fix | a **release** | everyone who updates |

A deploy runs the Deploy workflow in GitHub Actions. It tests, builds the site and updates moin.run.

A release has a version, like `v1.0.0-beta.38`, and its notes in the [changelog](/changelog). The tag `v…` starts it.

## A release

1. The notes go into the changelog, then the tag.
2. The Deploy workflow tests and builds the `moin` binaries.
3. It waits until a maintainer approves the run in GitHub.
4. It signs `SHA256SUMS` with `RELEASE_SIGNING_KEY`, a secret of the GitHub environment `release`, twice: `SHA256SUMS.sig` for `moin update`, `SHA256SUMS.sshsig` for `ssh-keygen` in `cli.sh`. Only a `v*` tag reaches the key, and only after that approval.
5. It makes the GitHub release as a draft, adds every file, and only then publishes it.

So nobody sees a new version before its signed release is complete.

## Updates

Nothing updates by itself. Not the CLI, not a server, not a watcher.

- `moin update` updates the CLI on your laptop, from the GitHub release, after it checked the signature.
- `moin update my-server` puts that same version on the server, over SSH. The server downloads it, checks its signature, and restarts the daemon. Your apps keep running.
- Once a day the CLI says when a new version is out.

What a release needs from you, if anything, is in its notes, under Upgrade. [Updates](/docs/security#updates)

## Your version

```bash
moin version
```
