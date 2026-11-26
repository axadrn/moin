---
title: "Account and connections"
description: "Your name, email, GitHub and passkey sign-in, sessions, connections and CI tokens"
group: "Team"
order: 3
---

Your account is you in moin: your name, your email and how you sign in. It lives under **Settings**, **Account**.

`moin login` connects your computer through the browser. You pick what it may do and allow it with your passkey. An AI agent signs in the same way, from the URL of your hub's MCP server: [AI agents](/docs/guides/mcp). Every client you let in is a **connection**: you see it, change its rights and remove it. CI and scripts use a **CI token**, a key you make by hand.

```bash
moin login
```

## Connect the CLI

`moin login` prints a code and opens `/device` on your hub. Sign in there. Check the code is the same. Pick the workspace and the [rights](/docs/reference/rights). Press **Allow with passkey**.

```surfaces
Web: `/device`, **Allow with passkey** or **Cancel**
CLI: moin login [--url <url>]
TUI: `moin` without a login shows the code and opens the browser
API: POST /api/v1/auth/device, then POST /api/v1/auth/token
```

Only allow a code you started yourself. Someone who sends you a link to `/device` wants your access.

- The rights start at read, deploy and env. Delete and servers are off. Turn on what this computer needs.
- **Allow** needs your passkey from the last five minutes, also when you just signed in. An account without a passkey allows within five minutes of signing in. After that the page says **Confirm it's you**: continue with GitHub, and you are back on the page, still signed in. [Add a passkey](#add-a-passkey) to approve faster.
- No browser here? Open the address moin prints on any other device.
- The code is good once, for ten minutes.
- The CLI keeps the login in its config file, readable only by you. It renews itself every hour.
- A connection unused for 90 days ends. Every use starts the 90 days again.
- Logging in again on the same computer ends its old connection. The TUI uses the same login.

## See your connections

Every client you let in, what it may do and when it was last used. A row opens the connection.

```surfaces
Web: **Settings**, **Connections**
CLI: moin connections
TUI: `w`, then `l` opens it in the web
API: GET /api/v1/account/connections
MCP: whoami, the rights of its own connection
```

## Change what a connection may do

Each right is a switch. It counts from the connection's next request, and a live stream comes back with it within the hour. Only you, signed in in the browser, change rights: no connection gives itself more.

Turning a right on asks you to confirm it's you, at the top of the page: **Use passkey**, or without a passkey **Continue with GitHub**. You stay signed in, and it counts for five minutes. Turning a right off never asks.

```surfaces
Web: **Settings**, **Connections**, the connection, **Rights**
API: PATCH /api/v1/account/connections/{id} with a session
```

[Rights](/docs/reference/rights) lists what each right covers, endpoint by endpoint.

## Remove a connection

Its tokens stop at once. `moin logout` removes the connection of this computer and forgets it.

```surfaces
Web: **Settings**, **Connections**, the connection, **Remove**
CLI: moin connections rm <id>, or moin logout for this computer
API: DELETE /api/v1/account/connections/{id}
```

## Make a CI token

For CI, the GitHub Action and scripts. A CI token belongs to one workspace and has its own rights, read, deploy and env at first. It can reach one app only, and it can expire.

```surfaces
Web: **Settings**, **Connections**, **New CI token**
API: POST /api/v1/ci-tokens with a session
```

Making one asks you to confirm first, as turning a right on does.

moin shows the token once. Give it to the CLI with `moin login --token <token>`, on stdin with `echo "$TOKEN" | moin login`, or as `MOIN_TOKEN` in CI.

- Its page changes its rights and removes it. Only the person who made it changes its rights.
- You see your own CI tokens. The workspace's owner sees every one and can remove any. A connection or CI token removes another person's only with the servers right.
- Someone who leaves the workspace, or is removed from it, loses their connections and CI tokens there at once.
- A connection unused for 90 days ends. It leaves the list and frees its place.
- A CI token for one app reaches only that app's endpoints. MCP refuses it.

## Remove a CI token

Requests with it stop at once.

```surfaces
Web: **Settings**, **Connections**, the token, **Remove**
CLI: moin connections rm <id>
API: DELETE /api/v1/ci-tokens/{id}
```

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
- Then moin asks for its name, filled in with your password manager's, like 1Password, or "Passkey" when moin does not know it. Keep it or type your own. Your password manager lists it under your hub's address, like `moin.run`.
- moin emails you when a passkey is added, so one you did not add does not go unseen.
- Adding one needs a confirmation from the last five minutes: with a passkey you have, or without one a sign-in or GitHub. Otherwise **Add passkey** asks **Confirm it's you**, and you stay signed in. A passkey lets clients in, so a stolen session cannot add its own.
- Until you have one, every page asks: **Secure your account: add a passkey.**
- On the sign-in page, **Sign in with a passkey**. No email to type: the passkey knows your account.

## See, rename and remove passkeys

The card lists each passkey by name, when you added it and when it last signed you in. **Rename** gives one a new name, 1 to 64 characters. **Remove** ends one.

```surfaces
Web: **Settings**, **Account**, **Security** tab, **Passkeys**, **Rename** or **Remove**
API: GET /api/v1/account/passkeys
```

`PATCH /api/v1/account/passkeys/{id}` with `{"name": "Work laptop"}` renames one. `DELETE /api/v1/account/passkeys/{id}` removes one. Your last way in stays: without GitHub, moin keeps your last passkey. Add another first.

## Connect GitHub

An account without GitHub, such as after a recovery key, connects one here. From then on you sign in with it. A GitHub account that signs in to another account cannot be connected.

```surfaces
Web: **Settings**, **Account**, **Security** tab, **GitHub**, **Connect GitHub**
```

The card shows **Connected as** and your GitHub login once it is done. `github_login` in `GET /api/v1/account` says the same.

## Lost your GitHub account or passkey

Sign in the other way, if you have one. Else ask the admin of your hub for a recovery key. They run `./main recovery-key <email>` on the server. Enter the key at `/auth/key`. It works once, for an hour.

The key resets every way in. It signs you out everywhere, ends your connections and CI tokens, disconnects GitHub and deletes your passkeys, so a lost GitHub account or device no longer gets in. Then connect your GitHub and add a passkey again under **Settings**, **Account**, **Security**. [Recovery key](/docs/self-hosting#recovery-key)

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

Notices and alerts go to the new address once you click the link moin sends there. The link signs you out everywhere. A browser signed in to this account stays in; anywhere else, sign in again with GitHub or a passkey. The link never signs anyone in.

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

Removes your account, passkeys, sessions, connections and CI tokens, and takes you out of every workspace. You type `DELETE` first. It cannot be undone. Servers, apps and env vars belong to the workspace and stay for your team. As the last owner, hand it on or [delete the workspace](/docs/guides/team#leave-or-delete-a-workspace) first.

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

- A connection or CI token with the servers right can change your name, picture and email alerts. None can ever change your email, how you sign in, passkeys, sessions, rights or tokens, or delete the account. So a leaked token never takes over your account.
