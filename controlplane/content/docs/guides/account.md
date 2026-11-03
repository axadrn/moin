---
title: "Account and tokens"
description: "Your name, email, password, sessions and API tokens"
order: 16
---

**The short version:** your account lives under **Settings**. Change it on the web. The CLI, the TUI and agents work with a personal API token. A token with write access can change your name, picture and email alerts, never your email, password, sessions or tokens.

## Sign up and sign in

Sign in with GitHub, with a link ops emails you, or with a password once you set one. The first sign-in asks for your name and the name of your first workspace. After GitHub sign-in both are filled in from your GitHub profile; with an email link you type them. Someone invited to a workspace gives only their name and joins that workspace.

```surfaces
Web: the sign-in page, **Continue with GitHub** or **Continue with email**
API: POST /api/v1/auth/onboarding
```

The API takes `name` and an optional `workspace_name`; without it the workspace is called "<name>'s Workspace".

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

## Email alerts

A mail when a site goes down, a server goes offline or a deploy fails, in the workspaces you own or run as admin. On by default; see [Alerts](/docs/guides/alerts) to turn it off.

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

The dialog starts at **Read and write**, which the CLI needs. A read only token cannot add servers, deploy or read rows and env values. Pick **Read only** for a script or agent that should only look.

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

Removes your account, your profile, your picture, your sessions and your API tokens, and takes you out of every workspace. It cannot be undone, so you type `DELETE` first.

Your servers, apps and env vars belong to a workspace, not to you, so they stay for your team. If you are the last owner of a workspace, hand it to someone else or [delete the workspace](/docs/guides/team#leave-or-delete-a-workspace) first. Deleting a workspace removes its servers and apps from ops; the apps keep running on your servers.

```surfaces
Web: **Settings**, **Security** tab, **Delete account**
API: DELETE /api/v1/account
```

## Why only the web

An API token, and so the CLI, the TUI and agents, cannot change your email, password, sessions or tokens, and cannot delete the account. Sign in on the web for that. A leaked token can never take over your account.

## Billing

On hosted ops you pay per workspace: $29 a month or $249 a year, with unlimited servers, apps and teammates. Your first workspace starts with 30 days free, without a card. When the trial ends and nobody has subscribed, the workspace is read-only: your apps and their backups keep running, and you can still see everything and stop and start apps, but not deploy or add servers until you subscribe.

Only the owner of the workspace sees the plan and pays, on the web: **Billing** in the sidebar. Buying and cancelling happen only on the web.

```surfaces
Web: **Billing** in the sidebar
API: GET /api/v1/workspaces/{slug}/billing
MCP: get_billing
```

A self-hosted control plane has no billing: every feature is free.
