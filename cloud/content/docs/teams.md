---
title: "Teams"
description: "Who may do what. Everyone with a key on the server may do everything. Add and remove people with moin keys. Let others deploy with a push to GitHub."
group: "Servers and data"
order: 3
---

Your team is the SSH keys on the server. Whoever has a key there is on the team.

## One key, everything

A key logs in as root. So everyone with a key may do everything on that server: deploy, read env vars, restore, delete.

```bash
moin keys my-server
moin keys add my-server anna.pub
moin keys rm my-server anna@laptop
```

- `moin keys my-server` lists the keys, by name and fingerprint.
- `moin keys add my-server anna.pub` lets Anna in.
- `moin keys rm my-server anna@laptop` takes her out. She can't log in from then on. It asks first, and needs `-y` in a script.

## Deploy without a key

Not everyone needs root. A designer or a contractor may only need to ship.

1. Give them push rights on the GitHub repo.
2. Turn on `moin follow` for the app: `moin follow my-app --branch main`.
3. Protect the branch on GitHub. Pull request, review, then merge.

Now a merge to `main` deploys to the server within a minute. The person never gets a key. Whoever can push to that branch can deploy, so protect it. [Deploy from CI](/docs/deploy#deploy-from-ci)

Does your CI build an image? `moin follow my-app --image ghcr.io/you/my-app:main` deploys each image it pushes. CI gets no key to the server either. [Deploy after your tests pass](/docs/deploy#deploy-after-your-tests-pass)

## Why no roles per key

A deploy runs code on the server as root. Whoever may deploy may do anything. A role "can deploy, can't read env" would be a lock on an open door.

So moin has no roles. The line is simple: a key is root, or it is not there. The branch protection on GitHub is where you decide who ships.

## What is coming

A log of who did what: which key deployed, rolled back or restored, and when. [Roadmap](/docs/roadmap)
