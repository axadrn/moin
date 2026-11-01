---
title: "API"
description: "The JSON API under /api/v1"
order: 3
---

The web UI, the CLI and the terminal UI all use this API. Its OpenAPI 3.1 document is served at [`/api/v1/openapi.yaml`](/api/v1/openapi.yaml).

Send a personal API token as `Authorization: Bearer pat_…`. The token belongs to one workspace, and every endpoint below works in it. Errors are `{"error":{"code","message","fields"}}` with stable codes such as `server_offline`, `deploy_running` and `no_rollback`.

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/activity` | who did what in the workspace, newest first, with `next_cursor` |
| `GET /api/v1/workspaces/{slug}` · `POST /{slug}/invitations` | the team and its invitations |
| `GET /api/v1/servers` · `POST` · `DELETE /{id}` | servers, with their `rules` from `/etc/ops/rules` |
| `GET /api/v1/servers/events` | server list as server-sent events |
| `GET /api/v1/apps` · `POST` · `GET /{app}` · `DELETE /{app}` | apps |
| `GET /api/v1/apps/events` | app list as server-sent events |
| `POST /api/v1/apps/{app}/uploads` | a link that takes the source straight into your bucket |
| `POST /api/v1/apps/{app}/deploys` | deploys an upload `{"upload": "…", "sha256": "…"}`, an image `{"image": "…"}` built in CI, or `{"ref": "main"}` from GitHub |
| `POST /api/v1/apps/{app}/hook` | a new deploy hook URL, shown once |
| `POST /webhooks/deploy/{token}` | the deploy hook CI calls with `?image=` |
| `GET /api/v1/registries` · `PUT` · `DELETE /{host}` | registry logins for private images |
| `GET /api/v1/apps/{app}/deploys/{number}` | a deploy with its log |
| `GET /api/v1/apps/{app}/deploys/{number}/events` | a deploy as server-sent events |
| `POST /api/v1/apps/{app}/redeploy` | builds the newest source again |
| `POST /api/v1/apps/{app}/rollback` | starts an earlier release again |
| `GET /api/v1/apps/{app}/logs` | container logs |
| `PUT /api/v1/servers/{id}/security` | Cloudflare Tunnel, SSH on Tailscale, firewall |
| `GET` · `PUT /api/v1/integrations` | the Cloudflare API token and the Tailscale auth key |
| `GET /api/v1/github` · `GET /api/v1/github/repos` | GitHub installations and install link; repos ops reads |
| `PUT` · `DELETE /api/v1/apps/{app}/repo` | links or unlinks a GitHub repo and branch |
| `GET /api/v1/apps/{app}/databases` | SQLite databases of the live release |
| `GET /api/v1/apps/{app}/databases/tables` | tables, columns, row counts, schema and migration |
| `GET` · `POST` · `PATCH` · `DELETE /api/v1/apps/{app}/databases/rows` | browse, add, edit and delete rows |
| `POST /api/v1/apps/{app}/databases/query` | runs SQL; `write: true` for changes |
| `GET` · `POST /api/v1/apps/{app}/backups` | backups; a snapshot now |
| `PUT /api/v1/apps/{app}/backups/settings` | snapshot before every change |
| `POST /api/v1/apps/{app}/restore` | restores a snapshot or a point in time |
| `GET` · `PUT` · `DELETE /api/v1/backup-storage` | the bucket backups go to |
| `GET` · `PUT /api/v1/apps/{app}/domains` | DNS checks; domains and canonical |
| `PUT` · `DELETE /api/v1/apps/{app}/protection` | password protection |
| `POST /api/v1/apps/{app}/apply` | restarts the running release with the current settings |
| `GET` · `PATCH /api/v1/apps/{app}/env` | env vars |
