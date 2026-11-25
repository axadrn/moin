---
title: "Account and tokens"
description: "Your name, email, GitHub and passkey sign-in, sessions, the CLI login and API tokens"
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

You sign up with GitHub. No passwords, no links by email. The first sign-in asks for your name and your first workspace's name. Someone invited gives only a name.

Then you sign in with GitHub or with a [passkey](#add-a-passkey).

```surfaces
Web: the sign-in page, **Continue with GitHub** or **Sign in with a passkey**
API: POST /api/v1/auth/onboarding
```

- Your GitHub account is your key. moin remembers it, not its email: change your email or name on GitHub and you still get in.
- An account from before GitHub finds yours by GitHub's verified primary email, once.
- An invitation lets you in with any GitHub account. [Team](/docs/guides/team#invite-someone)
- How it stays safe: [Security](/docs/guides/security#sign-in).

## Add a passkey

A passkey is a second way in, next to GitHub. It lives on your phone, laptop or password manager, and you unlock it with your face, finger or screen lock. Add one. Everyone should: nobody can phish it, and it lets you in when GitHub is down or lost. [Why](/docs/guides/security#add-a-passkey-everyone-should)

```surfaces
Web: **Settings**, **Account**, **Security** tab, **Passkeys**, **Add passkey**
```

- Your browser asks where to keep it. Pick your password manager to have it on every device.
- moin emails you when a passkey is added, so one you did not add does not go unseen.
- Until you have one, every page asks: **Secure your account: add a passkey.**
- On the sign-in page, **Sign in with a passkey**. No email to type: the passkey knows your account.

## See and remove passkeys

The card lists each passkey, when you added it and when it last signed you in. **Remove** ends one.

```surfaces
Web: **Settings**, **Account**, **Security** tab, **Passkeys**, **Remove**
API: GET /api/v1/account/passkeys
```

`DELETE /api/v1/account/passkeys/{id}` removes one. Your last way in stays: without GitHub, moin keeps your last passkey. Add another first.

## Connect GitHub

An account without GitHub, such as after a recovery key, connects one here. From then on you sign in with it. A GitHub account that signs in to another account cannot be connected.

```surfaces
Web: **Settings**, **Account**, **Security** tab, **GitHub**, **Connect GitHub**
```

The card shows **Connected as** and your GitHub login once it is done. `github_login` in `GET /api/v1/account` says the same.

## Lost your GitHub account or passkey

Sign in the other way, if you have one. Else ask the admin of your hub for a recovery key. They run `./main recovery-key <email>` on the server. Enter the key at `/auth/key`. It works once, for an hour.

The key resets every way in. It signs you out everywhere, disconnects GitHub and deletes your passkeys, so a lost GitHub account or device no longer gets in. Then connect your GitHub and add a passkey again under **Settings**, **Account**, **Security**. [Recovery key](/docs/self-hosting#recovery-key)

```surfaces
Web: `/auth/key`, **Key from the terminal**, **Sign in**
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

Links and notices go to the new address once you click the link moin sends there. The link signs you out everywhere. A browser signed in to this account stays in; anywhere else, sign in again with GitHub or a passkey. The link never signs anyone in.

```surfaces
Web: **Settings**, **Account**, **Profile** tab, **Email**, **Save**
API: PATCH /api/v1/account/email
```

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

Removes your account, passkeys, sessions and API tokens, and takes you out of every workspace. You type `DELETE` first. It cannot be undone. Servers, apps and env vars belong to the workspace and stay for your team. As the last owner, hand it on or [delete the workspace](/docs/guides/team#leave-or-delete-a-workspace) first.

```surfaces
Web: **Settings**, **Account**, **Security** tab, **Delete account**
API: DELETE /api/v1/account
```

## Billing

On hosted moin you pay per workspace: $7 a month or $49 a year, with unlimited servers, apps and teammates. Your price stays as long as your subscription runs. Your first workspace gets 30 days free, starting when you connect your first server. No card. After the trial without a subscription, the workspace is read-only. Apps and backups keep running. You cannot deploy or add servers.

```surfaces
Web: **Settings**, **Billing**
API: GET /api/v1/workspaces/{slug}/billing
MCP: get_billing
```

Only the owner sees the plan and pays. A self-hosted hub has no billing.

## Good to know

- A token with write access can change your name, picture and email alerts. It can never change your email, how you sign in, passkeys, sessions or tokens, or delete the account. So a leaked token never takes over your account.
