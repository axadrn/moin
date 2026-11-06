---
title: "API"
description: "The JSON API under /api/v1"
order: 3
---

The CLI and the terminal UI use this API, and so can your own scripts. [Where things work](/docs/reference/surfaces) shows which endpoints answer only a signed-in browser. Its OpenAPI 3.1 document is served at [`/api/v1/openapi.yaml`](/api/v1/openapi.yaml).

Send a personal API token as `Authorization: Bearer pat_…`. The token belongs to one workspace, and every endpoint below works in it. Errors are `{"error":{"code","message","fields"}}` with stable codes such as `server_offline`, `deploy_running` and `no_rollback`.

Endpoints marked *session* answer only a signed-in browser, never a token, so a leaked token cannot make new tokens, change roles, remove people or delete a workspace. Their sign-in and two-step flows live under `/api/v1/auth`; `GET /api/v1/csrf` gives the CSRF token a session sends with every change.

A user can hold 20 server-sent event streams open at once, across all `…/events` endpoints. One more answers `429` with `rate_limited`; closing a stream frees its place.

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/meta` | the name, version and support address of this control plane; needs no token |
| `GET /api/v1/version` | the version and, on a self-hosted control plane, the newest release, its "In short" line, the notes and how the last update went |
| `POST /api/v1/version/update` | updates a self-hosted control plane to the newest release (admins in `ADMIN_EMAILS`); follow it with `GET /api/v1/version` |
| `GET /api/v1/account` · `PATCH /account/name` | the signed-in account; its name |
| `PUT /api/v1/account/email-alerts` | turns your [email alerts](/docs/guides/alerts) on or off `{"on": false}`; `GET /api/v1/account` has them as `email_alerts` |
| `PATCH /api/v1/account/email` · `POST /account/password` · `DELETE /account` | email, password, deleting the account (*session*) |
| `POST /api/v1/account/password/set` · `DELETE /account/password` | sets the first password; removes it (*session*) |
| `POST` · `DELETE /api/v1/account/avatar` | the profile picture |
| `POST /api/v1/auth/device` · `POST /api/v1/auth/device/token` | `moin login` through the browser (RFC 8628): start a login with `{"hostname"}`, then poll with `{"device_code"}` every `interval` seconds until it answers `access_token`; needs no token. See [Log in the CLI](/docs/guides/account#log-in-the-cli) |
| `GET` · `POST /api/v1/account/tokens` · `DELETE /{id}` | personal API tokens and the computers logged in with `moin login`, `kind` `personal` or `device` (*session*) |
| `GET /api/v1/account/sessions` · `DELETE /{id}` · `DELETE` | signed-in browsers; signs one or all others out (*session*) |
| `GET /api/v1/activity` | who did what in the workspace, newest first, with `next_cursor`; `app`, `server` and `user` filter it |
| `GET /api/v1/workspaces` · `POST` | the workspaces you are in; a new one (*session*) |
| `GET /api/v1/workspaces/{slug}` | the team: members, roles and open invitations |
| `GET /api/v1/workspaces/{slug}/activity` | the same as `GET /api/v1/activity`, for the workspace in the path |
| `GET /api/v1/invitations/{token}` · `POST /{token}/accept` | an invitation from its email link; accepting it (*session*) |
| `PATCH /api/v1/workspaces/{slug}/name` | renames the workspace (admin) |
| `DELETE /api/v1/workspaces/{slug}` | deletes the workspace (owner, *session*) |
| `POST /api/v1/workspaces/{slug}/invitations` | invites someone by email (admin) |
| `POST /api/v1/workspaces/{slug}/invitations/{id}/resend` · `DELETE /{id}` | sends an invitation again; revokes it (admin) |
| `PATCH /api/v1/workspaces/{slug}/members/{id}/role` | changes a role (admin; only an owner makes or changes an owner, *session*) |
| `DELETE /api/v1/workspaces/{slug}/members/{id}` | removes a member (admin; only an owner removes an owner, *session*) |
| `POST /api/v1/workspaces/{slug}/leave` | leaves the workspace (*session*) |
| `GET /api/v1/workspaces/{slug}/billing` | the plan and the subscription, where billing is on |
| `POST /api/v1/workspaces/{slug}/billing/checkout` · `GET /billing/manage` | a checkout link; the billing portal (owner, *session*) |
| `GET /api/v1/servers` · `POST` | servers, with their `rules` from `/etc/moin/rules` |
| `GET /api/v1/servers/events` | server list as server-sent events |
| `POST /api/v1/servers/{id}/install` | a new one-time install command; the old one stops working |
| `PATCH /api/v1/servers/{id}` | renames a server `{"name": "…"}` |
| `DELETE /api/v1/servers/{id}` | removes a server; answers the `uninstall_command` that removes its agent there, empty when no agent ever connected, and the `apps` that went with an offline server |
| `POST /api/v1/servers/{id}/update` | updates the agent to the control plane's version |
| `GET /api/v1/servers/{id}/metrics?range=1h` | CPU, memory, disk, network and load of the server now and over a range, and its apps now, the most memory first |
| `GET /api/v1/agent-updates` · `PUT` | whether agents update by themselves `{"auto": true}`; on by default |
| `GET /api/v1/status` | whether everything is OK: `ok`, the `issues` that need you with the `path` to fix them, catalog `updates` and `counts` ([Overview](/docs/guides/overview)) |
| `GET /api/v1/catalog` · `GET /{slug}` | the [catalog](/docs/guides/catalog) with each app's newest release, or one app with every release |
| `POST /api/v1/catalog/{slug}/install` | installs a catalog app `{"server": "web-1", "version": "3.4.1", "name": "stats", "domain": "stats.example.com"}`; answers the app and its deploy |
| `POST /api/v1/apps/{app}/update` | sets a catalog app to a release `{"version": "3.4.1"}`, the newest when empty, and deploys it |
| `GET /api/v1/apps` · `POST` · `GET /{app}` · `DELETE /{app}` | apps |
| `GET /api/v1/apps/events` | app list as server-sent events |
| `PUT /api/v1/apps/{app}/pin` | pins an app `{"pinned": true}` or unpins it; lists show pinned apps above the others that run fine |
| `POST /api/v1/apps/{app}/uploads` | a link that takes the source straight into your bucket |
| `POST /api/v1/apps/{app}/deploys` | deploys an upload `{"upload": "…", "sha256": "…"}`, an image `{"image": "…"}` built in CI, or `{"ref": "main"}` from GitHub |
| `POST /api/v1/apps/{app}/hook` | a new deploy hook URL, shown once |
| `POST /webhooks/deploy/{token}` | the deploy hook CI calls with `?image=` or the JSON body `{"image": "…"}` |
| `GET /api/v1/registries` · `PUT` · `DELETE /{host}` | registry logins for private images |
| `GET /api/v1/apps/{app}/deploys/{number}` | a deploy with its log |
| `GET /api/v1/apps/{app}/deploys/{number}/events` | a deploy as server-sent events |
| `POST /api/v1/apps/{app}/redeploy` | builds the newest source again, or runs the newest image again |
| `POST /api/v1/apps/{app}/rollback` | starts an earlier release again |
| `GET /api/v1/apps/{app}/logs` | container logs |
| `POST /api/v1/apps/{app}/exec` | [runs a command](/docs/guides/exec) as server-sent events: `output` pieces, then `done` with `exit_code`, `timed_out` and `truncated`; takes `{"command": [...]}` or `{"line": "..."}`, `service` and `timeout` in seconds (owner or admin) |
| `PUT /api/v1/servers/{id}/security` | Cloudflare Tunnel, SSH on Tailscale, firewall |
| `GET /api/v1/alert-destinations` · `POST` · `DELETE /{name}` | where [alerts](/docs/guides/alerts#slack-discord-telegram-and-webhooks) go besides email: Slack, Discord, Telegram or a webhook; `POST` takes `{"kind", "name", "url"}`, or `bot_token` and `chat_id` for Telegram, and `events`; a webhook answers with its `signing_secret` once (owner or admin to change) |
| `POST /api/v1/alert-destinations/{name}/test` | sends a test alert; `424` with `alert_send_failed` and the reason when the destination refuses it (owner or admin) |
| `GET` · `PUT /api/v1/integrations` | the Cloudflare API token and the Tailscale auth key |
| `GET /api/v1/github` · `GET /api/v1/github/repos` | GitHub installations with their `settings_url` and the install link; repos moin reads |
| `GET /api/v1/github/repos/{owner}/{repo}/branches` | the branches of a repo, the default first |
| `PUT` · `DELETE /api/v1/apps/{app}/repo` | links or unlinks a GitHub repo and branch |
| `GET /api/v1/apps/{app}/databases` | Databases of the live release: SQLite files and Postgres services |
| `GET /api/v1/apps/{app}/databases/tables` | tables, columns, row counts, schema and migration |
| `GET` · `POST` · `PATCH` · `DELETE /api/v1/apps/{app}/databases/rows` | browse, add, edit and delete rows |
| `POST /api/v1/apps/{app}/databases/query` | runs SQL; `write: true` for changes |
| `GET` · `POST /api/v1/apps/{app}/backups` | backups; a snapshot now |
| `PUT /api/v1/apps/{app}/backups/settings` | snapshot before every change |
| `POST /api/v1/apps/{app}/restore` | restores a snapshot or a point in time; `bucket` names an older bucket the snapshot is in |
| `GET /api/v1/apps/{app}/uptime` | uptime per domain: state, 24 hours, 7 and 30 days, the newest check and the last outages |
| `PUT /api/v1/apps/{app}/uptime/{domain}` | the path of a domain's check `{"path": "/health"}`, or pauses it `{"paused": true}` |
| `GET /api/v1/apps/{app}/metrics?range=1h` | CPU, memory and network of the app now and over `1h`, `24h`, `7d` or `30d`, with the deploys that went live |
| `GET` · `POST /api/v1/buckets` | the workspace's buckets, the default first, with the apps that use each; adds one |
| `GET` · `PUT` · `DELETE /api/v1/buckets/{name}` | one bucket; changes it (an empty `secret_key` keeps the stored one, `default: true` makes it the default); removes it, `409 bucket_in_use` while apps use it or it keeps older backups |
| `PUT /api/v1/apps/{app}/bucket` | the app's bucket `{"bucket": "client-a"}`, or `""` for the default; older backups stay in the old bucket |
| `GET` · `PUT` · `DELETE /api/v1/backup-storage` | the default bucket, for older clients |
| `GET` · `PUT /api/v1/apps/{app}/domains` | DNS checks with the server's `ipv4`, `ipv6` and the record to set as `advice`; domains and canonical, or with `service` the domains of another service |
| `GET` · `PUT /api/v1/apps/{app}/instant` | the instant address, its provider and password (`?show=true` with a token that may write); pick the provider or `off`, and `open` |
| `PUT` · `DELETE /api/v1/apps/{app}/protection` | password protection |
| `POST /api/v1/apps/{app}/restart` | starts the live release again with the current settings |
| `POST /api/v1/apps/{app}/stop` | takes the app offline; its data stays |
| `POST /api/v1/apps/{app}/start` | runs a stopped app again |
| `GET` · `PATCH` · `PUT /api/v1/apps/{app}/env` | env vars; `PUT` replaces them all at once and answers the keys `added`, `changed` and `removed` |
