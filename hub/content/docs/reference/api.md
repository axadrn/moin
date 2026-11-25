---
title: "API"
description: "The JSON API under /api/v1. Every endpoint, by area."
group: "Reference"
order: 5
---

The CLI, the terminal UI and your own scripts use this API. Its OpenAPI 3.1 document is at [`/api/v1/openapi.yaml`](/api/v1/openapi.yaml).

## Basics

| | |
| --- | --- |
| Token | the access token of a [connection](/docs/guides/account#connect-the-cli) (`at_…`, from `moin login`, renewed every hour) or a [CI token](/docs/guides/account#make-a-ci-token) (`pat_…`), as `Authorization: Bearer`. It belongs to one workspace; every endpoint below works in it |
| Rights | each endpoint needs one [right](/docs/reference/rights): read, deploy, env, delete or servers. Without it: `403` `missing_right` with the `right`. **no token** endpoints answer a signed-in browser only, or nobody signed in for the steps of signing in |
| Session | people sign in in the browser with GitHub or a passkey and get a session cookie, never a bearer token; `GET /api/v1/csrf` gives the CSRF token a session sends with every change; the `Moin-Workspace` header names the workspace |
| Errors | `{"error":{"code","message","fields"}}`, with stable codes such as `server_offline`, `deploy_running` and `no_rollback`. A secret goes straight to a server: `409` `server_offline` when it is offline, `409` `secret_missing` when a server lacks a key a request needs |
| Confirm | what is big and cannot be undone takes the name, typed out, as `?confirm=`: removing a server, an app or a bucket, deleting a workspace. Without it or with another name: `422` `confirm_mismatch` |
| Streams | `…/events` endpoints are server-sent events. A user holds at most 20 streams at once; one more answers `429` `rate_limited`, closing one frees its place. The stream of a connection or CI token ends when its access token does, within the hour, with the event `reconnect`: open it again with a fresh token, so new rights and a removal count |
| Limits | a connection or CI token sends at most 121 requests a minute, to the API and to MCP together. One more answers `429` `rate_limited` with `Retry-After`, the seconds to wait: "Too many requests: 121 a minute per connection or CI token. Try again in 20 seconds." A stream counts once, when it opens. It protects the hub, it is no plan limit; the browser is not counted |
| IDs | answers carry the `id` of a workspace, app, server or bucket in 32 characters, the UUID without dashes, as links do. `{app}`, `{id}`, `{name}` and `{slug}` in a path, and fields that name one, take its name, its ID or a link part like `my-app-<id>` ([Names and links](/docs/guides/names)) |

Endpoints for no token keep a leaked token from making new tokens, giving itself rights, changing roles, removing people or deleting a workspace. [Rights](/docs/reference/rights) lists the right of every endpoint. [Where things work](/docs/reference/surfaces) shows the same per feature.

## Hub

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/meta` | the name, version and support address of this hub |
| `GET /api/v1/openapi.yaml` | this API as OpenAPI 3.1 |
| `GET /api/v1/csrf` | the CSRF token of a session |
| `GET /api/v1/version` | the version and, on a self-hosted hub, the newest version, its "In short" line, the notes and how the last update went |
| `POST /api/v1/version/update` | updates a self-hosted hub to the newest version (admins in `ADMIN_EMAILS`); follow it with `GET /api/v1/version` |
| `GET /api/v1/status` | whether everything is OK: `ok`, the `issues` that need you with the `path` to fix them, catalog `updates` and `counts` ([Overview](/docs/guides/overview)) |

## Sign in

| Endpoint | What it does |
| --- | --- |
| `POST /api/v1/auth/device` | starts `moin login` through the browser (RFC 8628) with `{"hostname"}` |
| `POST /api/v1/auth/token` | the OAuth token endpoint (RFC 6749), a form: `grant_type=urn:ietf:params:oauth:grant-type:device_code` with `device_code` polls `moin login` every `interval` seconds until it answers `access_token` (an hour) and `refresh_token`; `grant_type=refresh_token` gets the next pair, once. A refresh token used twice ends the connection. Errors are OAuth's: `{"error": "authorization_pending"}`. See [Connect the CLI](/docs/guides/account#connect-the-cli) |
| `POST /api/v1/auth/revoke` | ends the connection of a `token`, a form (RFC 7009); `moin logout` calls it |
| `POST /api/v1/auth/email-change/verify` | confirms a new email and answers the user; every session of the account ends, only a caller signed in as that user gets a new one |
| `POST /api/v1/auth/onboarding` | the name of a new account and its first workspace, or none with an `invitation_token`; every other endpoint answers `onboarding_required` until it is done |
| `POST /api/v1/auth/logout` | signs this browser out |

Sign-in itself is in the browser, not JSON: `/auth/github` with GitHub, `POST /auth/github/connect` to connect GitHub to a signed-in account, `/auth/passkey` with a passkey, and `/auth/key` for a [recovery key](/docs/self-hosting#recovery-key). Adding a passkey is in the browser too, under **Settings**, **Account**, **Security**. [Sign in](/docs/guides/security#sign-in)

## Account

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/account` | the signed-in account, with `email_alerts` and `github_login` |
| `PATCH /api/v1/account/name` | your name |
| `POST` · `DELETE /api/v1/account/avatar` | uploads or removes your picture |
| `PUT /api/v1/account/email-alerts` | your [email alerts](/docs/guides/alerts) on or off: `{"on": false}` |
| `PATCH /api/v1/account/email` | changes your email |
| `DELETE /api/v1/account` | deletes your account |
| `GET /api/v1/account/connections` | the clients you let in from the browser, with `rights`; `current` marks the connection that asks. A connection sees those of its workspace |
| `PATCH /api/v1/account/connections/{id}` | changes its rights, `{"rights": ["deploy", "env"]}`, from a signed-in browser only. Adding a right needs a passkey confirmation or a sign-in from the last five minutes, else `403` `confirm_required` |
| `DELETE /api/v1/account/connections/{id}` | ends a connection and its tokens at once |
| `GET` · `POST /api/v1/ci-tokens` | the CI tokens of the workspace you see (yours; the owner's every one); makes one from a signed-in browser, `{"name", "rights", "app", "expires_in_days"}`, and answers it once; like adding a right it needs a fresh confirmation, else `403` `confirm_required` |
| `PATCH` · `DELETE /api/v1/ci-tokens/{id}` | changes its rights (only its maker, from a signed-in browser); removes it (yours, or as the owner any) |
| `GET /api/v1/account/sessions` · `DELETE /account/sessions/{id}` · `DELETE /account/sessions` | signed-in browsers; signs one, or all others, out |
| `GET /api/v1/account/passkeys` · `DELETE /account/passkeys/{id}` | your passkeys, never their keys; removes one, but not the last way in: `409` `last_way_in` for the only passkey of an account without GitHub |

## Workspace and team

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/workspaces` | the workspaces you are in |
| `POST /api/v1/workspaces` | makes a workspace |
| `GET /api/v1/workspaces/{slug}` | the team: who is in it, their roles, open invitations |
| `PATCH /api/v1/workspaces/{slug}/name` | renames the workspace (admin); its slug follows |
| `DELETE /api/v1/workspaces/{slug}?confirm=<name>` | deletes the workspace (owner) |
| `POST /api/v1/workspaces/{slug}/invitations` | invites someone by email (admin). A connection or CI token invites only members: `"role": "admin"` answers `403` `forbidden` |
| `POST /api/v1/workspaces/{slug}/invitations/{id}/resend` · `DELETE /invitations/{id}` | sends an invitation again; revokes it (admin) |
| `PATCH /api/v1/workspaces/{slug}/members/{id}/role` | changes a role (admin; only an owner makes or changes an owner) |
| `DELETE /api/v1/workspaces/{slug}/members/{id}` | removes someone from the team (admin; only an owner removes an owner) |
| `POST /api/v1/workspaces/{slug}/leave` | leaves the workspace |
| `GET /api/v1/invitations/{token}` | an invitation, from its email link |
| `POST /api/v1/invitations/{token}/accept` | accepts it |
| `GET /api/v1/activity` | who did what, newest first, with `next_cursor`; `app`, `server` and `user` filter it. The actor by name; `actor_email` only for admins and owners |
| `GET /api/v1/workspaces/{slug}/activity` | the same, for the workspace in the path |
| `GET /api/v1/workspaces/{slug}/billing` | the plan and the subscription, where billing is on |
| `POST /api/v1/workspaces/{slug}/billing/checkout` · `GET /billing/manage` | a checkout link, with `"business": true` to confirm you subscribe as a business (`business_only` without it); the billing portal (owner) |

## Servers

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/servers` | servers, with their `security` (what the daemon set up) and `rules` from `/etc/moin/rules` |
| `GET /api/v1/servers/events` | the server list as server-sent events |
| `POST /api/v1/servers` | adds a server and answers its install command once; its token connects one daemon within an hour. An empty name gets the next free `web-1`, `web-2` … |
| `POST /api/v1/servers/{id}/install` | a new one-time install command; the old one stops working |
| `PATCH /api/v1/servers/{id}` | renames a server: `{"name": "…"}` |
| `DELETE /api/v1/servers/{id}?confirm=<name>` | removes a server; answers the `uninstall_command` that removes its daemon (empty when no daemon ever connected) and the `apps` that went with an offline server |
| `POST /api/v1/servers/{id}/update` | updates the daemon to the hub's version |
| `GET` · `PUT /api/v1/daemon-updates` | whether daemons update by themselves, `{"auto": true}`; on by default |
| `GET /api/v1/servers/{id}/metrics?range=1h` | CPU, memory, disk, network and load now and over `1h`, `24h`, `7d` or `30d`, and its apps now, the most memory first |
| `PUT /api/v1/servers/{id}/security` | `tunnel`, `ssh` (`everyone`, `tailscale` or `off`) and `firewall` ([Security](/docs/guides/security)) |
| `GET` · `PUT /api/v1/integrations` | connect Cloudflare and Tailscale with their keys; an empty key disconnects. The Tailscale key goes to `server`, which keeps it; `tailscale` answers `servers` and `missing_on` ([Integrations](/docs/guides/security#integrations)) |

## Apps

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/apps` · `POST /api/v1/apps` | lists apps; adds one with its password on, and its answer holds `password`, user `moin`, shown this once |
| `GET /api/v1/apps/{app}` · `DELETE /api/v1/apps/{app}?confirm=<app>` | one app, with `changes_waiting`: the changes the next restart applies; removes it, its data stays on the server |
| `GET /api/v1/apps/events` | the app list as server-sent events |
| `PATCH /api/v1/apps/{app}` | renames an app: `{"name": "…"}`; links, data, backups and the instant address stay, the next restart gives the containers the name |
| `PUT /api/v1/apps/{app}/pin` | `{"pinned": true}` keeps the app above the others that run fine; `false` unpins it |
| `POST /api/v1/apps/{app}/restart` | starts the live deploy again with the current settings |
| `POST /api/v1/apps/{app}/stop` · `POST /apps/{app}/start` | takes the app offline, its data stays; runs it again |
| `GET /api/v1/apps/{app}/logs` | container logs |
| `GET /api/v1/apps/{app}/compose` | the [compose file](/docs/guides/compose#the-compose-file-moin-runs) of the live deploy: `{"number": 12, "compose_file": "…"}`, YAML without secret values; `no_compose_file` for a live deploy without one |
| `POST /api/v1/apps/{app}/exec` | [runs a command](/docs/guides/exec), answered as server-sent events: `output` pieces, then `done` with `exit_code`, `timed_out` and `truncated`. Takes `{"command": [...]}` or `{"line": "..."}`, `service` and `timeout` in seconds (owner or admin) |
| `GET /api/v1/apps/{app}/metrics?range=1h` | CPU, memory and network now and over `1h`, `24h`, `7d` or `30d`, with the deploys that went live |

## Deploys

| Endpoint | What it does |
| --- | --- |
| `POST /api/v1/apps/{app}/deploys` | deploys an image built in CI `{"image": "…"}`, with the compose file of `"ref"` in the linked repo, its branch by default; or a branch, tag or commit of the linked GitHub repo `{"ref": "main"}` |
| `GET /api/v1/apps/{app}/deploys/{number}` | one deploy with its log |
| `GET /api/v1/apps/{app}/deploys/{number}/events` | one deploy as server-sent events |
| `POST /api/v1/apps/{app}/redeploy` | builds the newest source again, or runs the newest image again |
| `POST /api/v1/apps/{app}/rollback` | starts an earlier deploy again |
| `POST /api/v1/apps/{app}/hook` · `DELETE /apps/{app}/hook` | a new deploy hook URL, shown once, which turns deploy on push off and keeps the repo; turns the hook off |
| `POST /webhooks/deploy/{token}` | what CI calls, with `?image=&commit=` or the body `{"image": "…", "commit": "…"}`. The commit's compose file comes from the linked repo, its branch without one; an app without a repo refuses a commit. Only a new tag of the app's `hook_image`, the image running now; another gets `422` |
| `GET /api/v1/registries` | registry logins for private images, each with the `servers` that keep it and `missing_on`, the servers that do not |
| `PUT` · `DELETE /api/v1/registries/{host}` | hands a server a login for a host like `ghcr.io`, `{"username", "password", "server"}`; the server keeps it, the hub only the host and username. `server` may be left out with one server. Removes it, also from the servers |
| `GET /api/v1/github` · `GET /api/v1/github/repos` | GitHub installations with their `settings_url` and the install link; the repos moin may read |
| `GET /api/v1/github/repos/{owner}/{repo}/branches` | the branches of a repo, the default first |
| `PUT` · `DELETE /api/v1/apps/{app}/repo` | links a GitHub repo and branch; `auto_deploy` turns the deploy hook off; unlinks it |

## Catalog

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/catalog` · `GET /api/v1/catalog/{slug}` | the [catalog](/docs/guides/catalog) with each app's newest version; one app with every version |
| `POST /api/v1/catalog/{slug}/install` | installs a catalog app: `{"server": "web-1", "version": "3.4.1", "name": "stats", "domain": "stats.example.com"}`; answers the app and its deploy, in `login` a password the first sign-in needs and in `password` the app's password, user `moin`, both shown this once |
| `POST /api/v1/apps/{app}/update` | sets a catalog app to a version, `{"version": "3.4.1"}`, the newest when empty, and deploys it |

## Domains

| Endpoint | What it does |
| --- | --- |
| `GET` · `PUT /api/v1/apps/{app}/domains` | the domains and canonical; with `service`, those of another service. `GET` answers DNS checks with the server's `ipv4`, `ipv6` and the record to set as `advice` |
| `GET` · `PUT /api/v1/apps/{app}/instant` | the instant address and its provider; picks the provider or `off` |
| `PUT` · `DELETE /api/v1/apps/{app}/protection` | turns the app's [password](/docs/guides/domains#password) on for every address, with `{"username", "password"}`, or without a password moin makes one and answers it in `password`, this once; turns it off |
| `GET /api/v1/apps/{app}/uptime` | uptime per domain: state, 24 hours, 7 and 30 days, the last visit, the last outages |
| `PUT /api/v1/apps/{app}/uptime/{domain}` | the path uptime visits on a domain, `{"path": "/health"}`, or `{"paused": true}` |

## Env vars

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/apps/{app}/env` | the env vars as `{"items": [{"key", "updated_at"}]}`; values are write-only, so no answer holds one |
| `PATCH /api/v1/apps/{app}/env` | sets and removes some on the app's server, which must be online; changes take effect with the next deploy, a rollback too |
| `PUT /api/v1/apps/{app}/env` | replaces them all at once; answers the keys `added`, `replaced` and `removed` |

## Data

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/apps/{app}/databases` | the databases of the live deploy: SQLite files and Postgres services |
| `GET /api/v1/apps/{app}/databases/tables` | tables, columns, row counts, schema and migration |
| `GET` · `POST` · `PATCH` · `DELETE /api/v1/apps/{app}/databases/rows` | browses, adds, edits and deletes rows |
| `POST /api/v1/apps/{app}/databases/query` | runs SQL; `write: true` for changes |

## Backups and bucket

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/apps/{app}/backups` | the app's `every_minutes` and `keep_days`, and per database `copied_at`, `checked_at`, `check_error`, `problem` in one sentence when it does not work, its `copies`, its `undos` and the `moments` to go back to |
| `PUT /api/v1/apps/{app}/backups` | how often and how long, `{"every_minutes": 15, "keep_days": 30}`: `every_minutes` 15, 60 or 1440, `keep_days` 1, 3, 7 or 30 |
| `POST /api/v1/apps/{app}/restores` | goes back to a moment, `{"moment": "before-v12"}` (its id or label), or a time, `{"at": "2026-11-07T14:01:00Z"}`: the newest copy at or before it; `database` when the app has several, and `confirm`, the app's name typed out by the user. `202` with the restore; `422` `confirm_mismatch` without the exact name; `409` `restore_running` while one runs |
| `GET /api/v1/apps/{app}/restores/{restore}` | how a restore stands: `running`, `unknown` while its server is offline, then `done` or `failed` with `error`; kept a week. `POST /api/v1/apps/{app}/restore` is gone |
| `POST /api/v1/apps/{app}/backups/download` | a link to a copy in the app's own bucket, `{"database": "/data/app.db", "moment": "before-v12"}`, both optional: the newest copy of the only database. The app's server signs the link, so it must be online. `200` with `copy`, `label`, `at`, `size` and `link`, valid 15 minutes |
| `GET /api/v1/buckets` · `POST /api/v1/buckets` | the workspace's buckets, the default first, with the apps that use each, the `servers` that keep its key and `missing_on`; adds one: `server` tests it and keeps the `secret_key` |
| `GET` · `PUT` · `DELETE /api/v1/buckets/{name}` | one bucket; changes it (`server` tests a new place or key, an empty `secret_key` keeps the server's, `default: true` makes it the default); removes it with `?confirm=<name>`, `409` `bucket_in_use` while apps use it or left it |
| `PUT /api/v1/buckets/{name}/key` | hands one more server the bucket's key, `{"server", "secret_key"}`; it tests the bucket first |
| `PUT /api/v1/apps/{app}/bucket` | the app's bucket, `{"bucket": "client-a"}`, or `""` for the default; the copies start again there |

## Alerts

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/alert-destinations` | where [alerts](/docs/guides/alerts#slack-discord-telegram-and-webhooks) go besides email |
| `POST /api/v1/alert-destinations` | adds Slack, Discord, Telegram or a webhook: `{"kind", "name", "url"}`, or `bot_token` and `chat_id` for Telegram, and `events`; a webhook answers its `signing_secret` once (owner or admin) |
| `DELETE /api/v1/alert-destinations/{name}` | removes one (owner or admin) |
| `POST /api/v1/alert-destinations/{name}/test` | sends a test alert; `424` `alert_send_failed` with the reason when the destination refuses it (owner or admin) |

## Admin

For the admins of the hosted hub, behind `/admin`.

| Endpoint | What it does |
| --- | --- |
| `GET /api/v1/admin/users` | the admin overview: metrics, a page of users (`query` filters by email or name), the latest activity |
| `POST /api/v1/admin/users/{id}/sessions/revoke` | signs a user out everywhere |
| `GET /api/v1/admin/old-domain` | what still arrives on the old domains (`OLD_URLS`) over the last 14 days, by day, host and kind; `quiet` after 14 quiet days |
