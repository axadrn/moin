---
title: "Account and tokens"
description: "Your name, email, password, sessions and API tokens"
order: 16
---

**The short version:** your account lives under **Settings**. Change it on the web. `moin login` logs your computer in through the browser; CI, scripts and agents work with a personal API token. A token with write access can change your name, picture and email alerts, never your email, password, sessions or tokens.

## Sign up and sign in

Sign in with GitHub, with a link moin emails you, or with a password once you set one. The first sign-in asks for your name and the name of your first workspace. After GitHub sign-in both are filled in from your GitHub profile; with an email link you type them. Someone invited to a workspace gives only their name and joins that workspace.

```surfaces
Web: the sign-in page, **Continue with GitHub** or **Continue with email**
API: POST /api/v1/auth/onboarding
```

The API takes `name` and an optional `workspace_name`; without it the workspace is called "<name>'s Workspace".

## Change your name

The name your team sees in moin and in the activity.

```surfaces
Web: **Settings**, **Profile** tab, **Name**, **Save**
API: PATCH /api/v1/account/name
```

## Change your picture

```surfaces
Web: **Settings**, **Profile** tab, **Picture**
API: POST /api/v1/account/avatar
```

`DELETE /api/v1/account/avatar` takes it away again.

## Email alerts

A mail when a site goes down, a server goes offline or a deploy fails, in the workspaces you own or run as admin. On by default; see [Alerts](/docs/guides/alerts) to turn it off.

## Change your email

Sign-in links and notices go to the new address once you click the link moin sends there.

```surfaces
Web: **Settings**, **Profile** tab, **Email**, **Save**
API: PATCH /api/v1/account/email
```

## Set, change or remove your password

You can sign in with an email link, GitHub or a password. Without a password you sign in with links only.

```surfaces
Web: **Settings**, **Security** tab, **Password**
API: POST /api/v1/account/password
```

`POST /api/v1/account/password/set` sets the first password, `DELETE /api/v1/account/password` removes it.

## See and end sessions

Every browser you are signed in with is a session.

```surfaces
Web: **Settings**, **Security** tab, **Sessions**
API: GET /api/v1/account/sessions
```

**Revoke** on a session ends it; **Sign out everywhere** ends all of them, this one too, after a question. In the API, `DELETE /api/v1/account/sessions/{id}` ends one, `DELETE /api/v1/account/sessions` all others.

## Log in the CLI

`moin login` logs this computer in through the browser, like `gh auth login`. It prints a code and opens the page `/device` of your control plane. Sign in there with any way you like, check that the page shows the same code, pick the workspace if you have more than one, and press **Approve**. The terminal says `Logged in as` with your name. **Deny** stops the login; nothing gets access.

```surfaces
Web: `/device`, **Approve** or **Deny**
CLI: moin login [--url <url>]
TUI: `moin` without a login shows the code and opens the browser
API: POST /api/v1/auth/device, then POST /api/v1/auth/device/token
```

Without a browser on the machine, over SSH for example, moin prints the address instead: open it on any other device, your phone too. `--web` starts the browser login even when stdin is not a terminal. The code is good for ten minutes and once.

Only approve a code you started yourself. Someone who sends you a link to `/device` wants your access.

The login is a token of its own for this computer: read and write, bound to the workspace you picked, named like **CLI on laptop**, and marked **browser login** under **Settings**, **Tokens**. It ends after 90 days without use; every use starts the 90 days again. Logging in again on the same computer replaces its last login. The TUI uses the same login, and without one it starts this login itself: it shows the code, opens the browser and lands on the dashboard. The API answers the polls with the codes of RFC 8628: `authorization_pending`, `slow_down`, `access_denied`, `expired_token` and `invalid_grant`.

MCP has no login: agents send a personal API token, see [MCP](/docs/guides/mcp).

## Create an API token

CI, the GitHub Action, scripts and agents sign in with a personal API token. Each token belongs to one workspace and can read, or read and write.

The dialog starts at **Read and write**, which the CLI needs. A read only token cannot add servers, deploy or read rows and env values. Pick **Read only** for a script or agent that should only look.

```surfaces
Web: **Settings**, **Tokens** tab, **Create token**
API: POST /api/v1/account/tokens
```

moin shows the token once. Give it to the CLI with `moin login --token <token>`, on stdin with `echo "$TOKEN" | moin login`, or as `MOIN_TOKEN` in CI.

## See your API tokens

The list shows the tokens you made and the computers you logged in with `moin login`, with when each was last used and when it expires.

```surfaces
Web: **Settings**, **Tokens** tab
API: GET /api/v1/account/tokens
```

## Revoke an API token

Requests with it stop working right away. The web asks first. Revoke a computer's **CLI on** token when you lose it or stop using it. `moin logout` only forgets the token on your computer; it still works until you revoke it.

```surfaces
Web: **Settings**, **Tokens** tab, **Revoke** on the token
API: DELETE /api/v1/account/tokens/{id}
```

A token cannot be renamed or get more rights. Make a new one and revoke the old one: a token that leaked never gains power.

## Delete your account

Removes your account, your profile, your picture, your sessions and your API tokens, and takes you out of every workspace. It cannot be undone, so you type `DELETE` first.

Your servers, apps and env vars belong to a workspace, not to you, so they stay for your team. If you are the last owner of a workspace, hand it to someone else or [delete the workspace](/docs/guides/team#leave-or-delete-a-workspace) first. Deleting a workspace removes its servers and apps from moin; the apps keep running on your servers.

```surfaces
Web: **Settings**, **Security** tab, **Delete account**
API: DELETE /api/v1/account
```

## Why only the web

An API token, and so the CLI, the TUI and agents, cannot change your email, password, sessions or tokens, and cannot delete the account. Sign in on the web for that. A leaked token can never take over your account.

## Billing

On hosted moin you pay per workspace: $29 a month or $249 a year, with unlimited servers, apps and teammates. Your first workspace starts with 30 days free, without a card. When the trial ends and nobody has subscribed, the workspace is read-only: your apps and their backups keep running, and you can still see everything and stop and start apps, but not deploy or add servers until you subscribe.

Only the owner of the workspace sees the plan and pays, on the web: **Billing** in the sidebar. Buying and cancelling happen only on the web.

```surfaces
Web: **Billing** in the sidebar
API: GET /api/v1/workspaces/{slug}/billing
MCP: get_billing
```

A self-hosted control plane has no billing: every feature is free.
