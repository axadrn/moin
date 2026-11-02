---
title: "Account and tokens"
description: "Your name, email, password, sessions and API tokens"
order: 15
---

**The short version:** your account lives under **Settings**. Change it on the web. The CLI, the TUI and agents work with a personal API token, which can never change your account.

## Change your name

The name your team sees in ops and in the activity.

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

## Change your email

Sign-in links and notices go to the new address once you click the link ops sends there.

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

## Create an API token

The CLI, the TUI, the GitHub Action and agents sign in with a personal API token. Each token belongs to one workspace and can read, or read and write.

```surfaces
Web: **Settings**, **Tokens** tab, **Create token**
API: POST /api/v1/account/tokens
```

ops shows the token once. Then `ops login` on your computer asks for it.

## See your API tokens

```surfaces
Web: **Settings**, **Tokens** tab
API: GET /api/v1/account/tokens
```

## Revoke an API token

Requests with it stop working right away. The web asks first. `ops logout` only forgets the token on your computer; it still works until you revoke it.

```surfaces
Web: **Settings**, **Tokens** tab, **Revoke** on the token
API: DELETE /api/v1/account/tokens/{id}
```

A token cannot be renamed or get more rights. Make a new one and revoke the old one: a token that leaked never gains power.

## Delete your account

Removes your account, your profile and your API tokens. It cannot be undone, so you type `DELETE` first. If you are the last owner of a workspace, hand it to someone else or delete it first.

```surfaces
Web: **Settings**, **Security** tab, **Delete account**
API: DELETE /api/v1/account
```

## Why only the web

An API token, and so the CLI, the TUI and agents, cannot change your email, password, sessions or tokens, and cannot delete the account. Sign in on the web for that. A leaked token can never take over your account.

## Billing

On hosted ops you pay per workspace: $29 a month or $249 a year, with unlimited servers, apps and teammates. Your first workspace starts with 30 days free, without a card. When the trial ends and nobody has subscribed, the workspace is read-only: your apps and their backups keep running, and you can still see everything and stop and start apps, but not deploy or add servers until you subscribe.

Only the owner of the workspace sees the plan and pays, on the web: **Billing** in the sidebar. `GET /api/v1/workspaces/{slug}/billing` and the `get_billing` tool show the plan; buying and cancelling happen only on the web. A self-hosted control plane has no billing: every feature is free.
