---
title: "Account and tokens"
description: "Your name, email, password, sessions, the CLI login and API tokens"
group: "Team"
order: 3
---

Your account is you in moin: your name, your email and how you sign in. It lives under **Settings**, **Account**.

`moin login` logs your computer in through the browser. CI, scripts and AI agents use an API token, a key of their own.

```bash
moin login
```

## Log in the CLI

`moin login` prints a code and opens `/device` on your hub. Sign in there. Check the code is the same. Press **Approve**.

```surfaces
Web: `/device`, **Approve** or **Deny**
CLI: moin login [--url <url>]
TUI: `moin` without a login shows the code and opens the browser
API: POST /api/v1/auth/device, then POST /api/v1/auth/device/token
```

Only approve a code you started yourself. Someone who sends you a link to `/device` wants your access.

- No browser here? Open the address moin prints on any other device.
- The code is good once, for ten minutes.
- The login is a token for this computer, for one workspace.
- It ends after 90 days without use. Every use starts the 90 days again.
- The TUI uses the same login.

## Create an API token

For CI, the GitHub Action, scripts and AI agents. Each token belongs to one workspace. It can **Read only**, or **Read and write**, which the CLI needs.

```surfaces
Web: **Settings**, **Account**, **Tokens** tab, **Create token**
API: POST /api/v1/account/tokens
```

moin shows the token once. Give it to the CLI with `moin login --token <token>`, on stdin with `echo "$TOKEN" | moin login`, or as `MOIN_TOKEN` in CI.

## See your API tokens

The tokens you made and the computers you logged in with `moin login`.

```surfaces
Web: **Settings**, **Account**, **Tokens** tab
API: GET /api/v1/account/tokens
```

## Revoke an API token

Requests with it stop at once. Revoke a computer's **CLI on** token when you lose it. `moin logout` only forgets the token on your computer.

```surfaces
Web: **Settings**, **Account**, **Tokens** tab, **Revoke** on the token
API: DELETE /api/v1/account/tokens/{id}
```

A token cannot be renamed or get more rights: make a new one and revoke the old one.

## Sign up and sign in

With GitHub, with a link moin emails you, or with a password once you set one. The first sign-in asks for your name and your first workspace's name. Someone invited gives only a name.

```surfaces
Web: the sign-in page, **Continue with GitHub** or **Continue with email**
API: POST /api/v1/auth/onboarding
```

## Change your name


```surfaces
Web: **Settings**, **Account**, **Profile** tab, **Name**, **Save**
API: PATCH /api/v1/account/name
```

## Change your picture

```surfaces
Web: **Settings**, **Account**, **Profile** tab, **Picture**, **Upload**
API: POST /api/v1/account/avatar
```

**Remove** next to it, or `DELETE /api/v1/account/avatar`, takes it away.

## Change your email

Links and notices go to the new address once you click the link moin sends there.

```surfaces
Web: **Settings**, **Account**, **Profile** tab, **Email**, **Save**
API: PATCH /api/v1/account/email
```

## Set, change or remove your password


```surfaces
Web: **Settings**, **Account**, **Security** tab, **Password**
API: POST /api/v1/account/password
```

`POST /api/v1/account/password/set` sets the first one, `DELETE /api/v1/account/password` removes it.

## See and end sessions

Every browser you are signed in with is a session. **Revoke** ends one. **Sign out everywhere** ends all.

```surfaces
Web: **Settings**, **Account**, **Security** tab, **Sessions**
API: GET /api/v1/account/sessions
```

`DELETE /api/v1/account/sessions/{id}` ends one, `DELETE /api/v1/account/sessions` all others.

## Email alerts

A mail when something breaks in the workspaces you own or run as admin, on by default: see [Alerts](/docs/guides/alerts).

## Delete your account

Removes your account, sessions and API tokens, and takes you out of every workspace. You type `DELETE` first. It cannot be undone. Servers, apps and env vars belong to the workspace and stay for your team. As the last owner, hand it on or [delete the workspace](/docs/guides/team#leave-or-delete-a-workspace) first.

```surfaces
Web: **Settings**, **Account**, **Security** tab, **Delete account**
API: DELETE /api/v1/account
```

## Billing

On hosted moin you pay per workspace: $29 a month or $249 a year, with unlimited servers, apps and teammates. Your first workspace starts with 30 days free, without a card. After the trial without a subscription, the workspace is read-only. Apps and backups keep running. You cannot deploy or add servers.

```surfaces
Web: **Settings**, **Billing**
API: GET /api/v1/workspaces/{slug}/billing
MCP: get_billing
```

Only the owner sees the plan and pays. A self-hosted hub has no billing.

## Good to know

- A token with write access can change your name, picture and email alerts. It can never change your email, password, sessions or tokens, or delete the account. So a leaked token never takes over your account.
