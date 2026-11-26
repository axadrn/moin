---
title: "Rights"
description: "What a connection or a CI token may do, and the right each endpoint needs"
group: "Reference"
order: 6
---

Every client that acts for you has rights: the CLI and the TUI of a computer, an AI agent, a CI token. You pick them when you let it in, and change them under **Settings**, **Connections**. A change counts from its next request. A live stream, such as the TUI's lists, comes back with fresh rights within the hour.

| Right | On at first | Lets it |
| --- | --- | --- |
| read | always, it cannot be off | look: apps, servers, deploys, logs, metrics, status, the compose file, env var names, database tables, the backup schedule and its copies, the live streams |
| deploy | on | run and change apps: create, rename, pin, deploy, redeploy, roll back, restart, start, stop, update from the catalog, domains, the instant address, uptime, how often backups run, the deploy hook, the repo, the app's bucket |
| env | on | an app's secrets and data, the switch "Secrets and data": set and remove env vars, its password, database rows, read-only SQL, a link to download a copy |
| delete | off | what is hard to get back: remove an app or a bucket, a server's apps, restore a database, change rows and run SQL that writes |
| servers | off | what is around the apps: add, rename and remove servers, update daemons, server access, integrations, buckets, registry logins, alert destinations, the team and the workspace's name, hub updates, your name, picture and email alerts |

- Rights never add to your role. A member's connection with every right still cannot invite anyone. And a connection or CI token invites only members: an admin is invited in the web.
- Without the right, the API answers `403` `missing_right` with the `right`, and the CLI and MCP say: "This connection may not deploy and restart. Change it in Settings, Connections."
- Removing a server needs servers and delete: an offline server takes its apps with it.
- Domains are one list: setting it needs deploy, also when it drops a domain.
- Rows, SQL and copies of a database are app data, as strong as secrets: they need env. Changing rows and SQL that writes (`write: true`) also need delete.
- Running a command in a container (`exec`) can do anything in the app, read its secrets and delete its data: it needs deploy, env and delete together.
- Read removes your connections and your own CI tokens: taking your own access away never needs more. Removing another person's CI token, as the owner, needs servers.
- Deploy runs new code in an app, and that code sees the app's secrets and data. Give it with care. On the server, [`deploy-from`](/docs/guides/security#rules) limits what code may come.
- With Postgres, SQL that writes can run commands in the database container.
- Servers can invite a member, and a member deploys, sets env and runs SQL. Give it only to a client you would let into the team.
- **no token**: a connection or CI token never calls it, whatever its rights: your sessions, passkeys and email, rights themselves, new CI tokens, roles, leaving or deleting a workspace or the account, billing, admin. A signed-in browser does, and nobody needs to be signed in for the steps of signing in.
- A CI token for one app reaches only the endpoints of that app, `GET /api/v1/account` and `GET /api/v1/meta`. Everything else answers `403` `app_only`, MCP too.

Giving more is guarded like letting a client in. Turning a right on and making a CI token need you to confirm it's you: with your passkey if you have one, else with GitHub or a sign-in. It counts for five minutes, so a few switches need it once. Turning a right off and removing never ask.

The MCP tools say their right in the [tools table](/docs/guides/mcp#tools) and at the end of their description.

## Endpoints

| Endpoint | Right |
| --- | --- |
| `GET /api/v1/openapi.yaml` | read |
| `GET /api/v1/csrf` | read |
| `GET /api/v1/meta` | read |
| `GET /api/v1/version` | read |
| `POST /api/v1/auth/email-change/verify` | no token |
| `POST /api/v1/auth/device` | no token |
| `POST /api/v1/auth/token` | no token |
| `POST /api/v1/auth/revoke` | no token |
| `POST /api/v1/auth/register` | no token |
| `POST /api/v1/auth/onboarding` | no token |
| `POST /api/v1/auth/logout` | no token |
| `GET /api/v1/account` | read |
| `PATCH /api/v1/account/name` | servers |
| `PUT /api/v1/account/email-alerts` | servers |
| `PATCH /api/v1/account/email` | no token |
| `DELETE /api/v1/account` | no token |
| `GET /api/v1/account/sessions` | no token |
| `DELETE /api/v1/account/sessions/{id}` | no token |
| `DELETE /api/v1/account/sessions` | no token |
| `GET /api/v1/account/passkeys` | no token |
| `PATCH /api/v1/account/passkeys/{id}` | no token |
| `DELETE /api/v1/account/passkeys/{id}` | no token |
| `GET /api/v1/account/connections` | read |
| `PATCH /api/v1/account/connections/{id}` | no token |
| `DELETE /api/v1/account/connections/{id}` | read |
| `GET /api/v1/ci-tokens` | read |
| `POST /api/v1/ci-tokens` | no token |
| `PATCH /api/v1/ci-tokens/{id}` | no token |
| `DELETE /api/v1/ci-tokens/{id}` | read |
| `POST /api/v1/account/avatar` | servers |
| `DELETE /api/v1/account/avatar` | servers |
| `GET /api/v1/workspaces` | read |
| `POST /api/v1/workspaces` | no token |
| `GET /api/v1/workspaces/{workspaceSlug}` | read |
| `DELETE /api/v1/workspaces/{workspaceSlug}` | no token |
| `PATCH /api/v1/workspaces/{workspaceSlug}/name` | servers |
| `POST /api/v1/workspaces/{workspaceSlug}/invitations` | servers |
| `POST /api/v1/workspaces/{workspaceSlug}/invitations/{invitationID}/resend` | servers |
| `DELETE /api/v1/workspaces/{workspaceSlug}/invitations/{invitationID}` | servers |
| `PATCH /api/v1/workspaces/{workspaceSlug}/members/{memberID}/role` | no token |
| `DELETE /api/v1/workspaces/{workspaceSlug}/members/{memberID}` | no token |
| `POST /api/v1/workspaces/{workspaceSlug}/leave` | no token |
| `GET /api/v1/workspaces/{workspaceSlug}/activity` | read |
| `GET /api/v1/activity` | read |
| `GET /api/v1/invitations/{token}` | read |
| `POST /api/v1/invitations/{token}/accept` | no token |
| `GET /api/v1/workspaces/{workspaceSlug}/billing` | read |
| `POST /api/v1/workspaces/{workspaceSlug}/billing/checkout` | no token |
| `GET /api/v1/workspaces/{workspaceSlug}/billing/manage` | no token |
| `GET /api/v1/servers` | read |
| `POST /api/v1/servers` | servers |
| `DELETE /api/v1/servers/{id}` | delete, servers |
| `PATCH /api/v1/servers/{id}` | servers |
| `POST /api/v1/servers/{id}/update` | servers |
| `POST /api/v1/servers/{id}/install` | servers |
| `PUT /api/v1/servers/{id}/security` | servers |
| `GET /api/v1/servers/{id}/metrics` | read |
| `GET /api/v1/integrations` | read |
| `PUT /api/v1/integrations` | servers |
| `GET /api/v1/daemon-updates` | read |
| `PUT /api/v1/daemon-updates` | servers |
| `GET /api/v1/servers/events` | read |
| `GET /api/v1/status` | read |
| `GET /api/v1/catalog` | read |
| `GET /api/v1/catalog/{slug}` | read |
| `POST /api/v1/catalog/{slug}/install` | deploy |
| `GET /api/v1/apps` | read |
| `POST /api/v1/apps` | deploy |
| `PUT /api/v1/apps/{app}/pin` | deploy |
| `GET /api/v1/apps/events` | read |
| `GET /api/v1/apps/{app}` | read |
| `PATCH /api/v1/apps/{app}` | deploy |
| `DELETE /api/v1/apps/{app}` | delete |
| `POST /api/v1/apps/{app}/deploys` | deploy |
| `GET /api/v1/apps/{app}/deploys/{number}` | read |
| `GET /api/v1/apps/{app}/deploys/{number}/events` | read |
| `POST /api/v1/apps/{app}/rollback` | deploy |
| `POST /api/v1/apps/{app}/update` | deploy |
| `POST /api/v1/apps/{app}/redeploy` | deploy |
| `POST /api/v1/apps/{app}/hook` | deploy |
| `DELETE /api/v1/apps/{app}/hook` | deploy |
| `POST /api/v1/apps/{app}/restart` | deploy |
| `POST /api/v1/apps/{app}/start` | deploy |
| `POST /api/v1/apps/{app}/stop` | deploy |
| `GET /api/v1/apps/{app}/domains` | read |
| `PUT /api/v1/apps/{app}/domains` | deploy |
| `GET /api/v1/apps/{app}/instant` | read |
| `PUT /api/v1/apps/{app}/instant` | deploy |
| `PUT /api/v1/apps/{app}/password` | env |
| `DELETE /api/v1/apps/{app}/password` | env |
| `GET /api/v1/registries` | read |
| `PUT /api/v1/registries/{host}` | servers |
| `DELETE /api/v1/registries/{host}` | servers |
| `GET /api/v1/apps/{app}/logs` | read |
| `GET /api/v1/apps/{app}/compose` | read |
| `POST /api/v1/apps/{app}/exec` | deploy, env, delete |
| `GET /api/v1/apps/{app}/databases` | read |
| `GET /api/v1/apps/{app}/databases/tables` | read |
| `GET /api/v1/apps/{app}/databases/rows` | env |
| `POST /api/v1/apps/{app}/databases/rows` | env, delete |
| `PATCH /api/v1/apps/{app}/databases/rows` | env, delete |
| `DELETE /api/v1/apps/{app}/databases/rows` | env, delete |
| `POST /api/v1/apps/{app}/databases/query` | env |
| `GET /api/v1/apps/{app}/backups` | read |
| `PUT /api/v1/apps/{app}/backups` | deploy |
| `POST /api/v1/apps/{app}/backups/download` | env |
| `POST /api/v1/apps/{app}/restores` | delete |
| `GET /api/v1/apps/{app}/restores/{restore}` | read |
| `GET /api/v1/apps/{app}/uptime` | read |
| `PUT /api/v1/apps/{app}/uptime/{domain}` | deploy |
| `GET /api/v1/alert-destinations` | read |
| `POST /api/v1/alert-destinations` | servers |
| `DELETE /api/v1/alert-destinations/{destination}` | servers |
| `POST /api/v1/alert-destinations/{destination}/test` | servers |
| `GET /api/v1/apps/{app}/metrics` | read |
| `GET /api/v1/github` | read |
| `GET /api/v1/github/repos` | read |
| `GET /api/v1/github/repos/{owner}/{repo}/branches` | read |
| `PUT /api/v1/apps/{app}/repo` | deploy |
| `DELETE /api/v1/apps/{app}/repo` | deploy |
| `GET /api/v1/buckets` | read |
| `POST /api/v1/buckets` | servers |
| `GET /api/v1/buckets/{name}` | read |
| `PUT /api/v1/buckets/{name}` | servers |
| `PUT /api/v1/buckets/{name}/key` | servers |
| `DELETE /api/v1/buckets/{name}` | delete |
| `PUT /api/v1/apps/{app}/bucket` | deploy |
| `GET /api/v1/apps/{app}/env` | read |
| `PATCH /api/v1/apps/{app}/env` | env |
| `PUT /api/v1/apps/{app}/env` | env |
| `GET /api/v1/admin/users` | no token |
| `GET /api/v1/admin/old-domain` | no token |
| `POST /api/v1/admin/users/{userID}/sessions/revoke` | no token |
