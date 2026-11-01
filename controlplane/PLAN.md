# ops plan

`ops` is a working name. It changes once before the public beta. Until then the
name lives in one place per repo (`internal/brand` here, `internal/name` in `ops`).

## Product

Hosting, deploys, backups and data for Go + SQLite apps, on the user's own
servers, from the browser, the CLI and the TUI. Every feature works the same in
all three.

## Pieces

```text
ops (TUI/CLI) ─┐
browser ───────┼──▶ control plane ◀── ops agent (server 1, server 2, …)
```

| Piece | Repo | Source | What it is |
| --- | --- | --- | --- |
| control plane | `axadrn/ops/controlplane` | open source (AGPL-3.0) | Go app, hosted by us or self-hosted. Accounts, billing, servers, apps, deploys, backups, audit log. Serves the web UI and the API. Agents connect to it. |
| `ops` | `axadrn/ops` | open source (AGPL-3.0) | One binary. `ops` opens the TUI, `ops <command>` is the CLI, `ops agent` is the agent on a server. |
| protocol | `axadrn/ops/protocol` | open source | Go package with the messages between agent and control plane. The control plane and the agent import it. |

We host the control plane. Anyone may self-host it: SQLite, billing off.

## Words

Use these words everywhere: code, UI, docs.

- **control plane**: the hosted app.
- **agent**: `ops agent` on a server.
- **server**: a machine with an agent. Not "node", not "host".
- **app**: the user's Go program on a server. One app has one SQLite database.
- **deploy**: one release of an app. Numbered per app (`v42`).
- **backup**: a copy of an app's database. Continuous (Litestream) or a snapshot.

## Connection

Kept from baselite.

- The agent dials out. Nothing on the server listens for ops.
- `POST /api/agent/register` trades a one-time install token for an agent token.
- `GET /api/agent/session` upgrades to one WebSocket (`coder/websocket`), `Authorization: Bearer <agent token>`.
- Envelope: `{"type": "...", "request_id": "...", "data": {...}}`. A request from the control plane carries a `request_id`; the agent answers with the same id.
- The agent reconnects with backoff. The control plane marks a server offline when the session drops and nothing came back within 30 seconds, and alerts.
- `hello` is the first agent message: agent version, protocol version, OS, arch, hostname.

Clients (web, TUI, CLI) never talk to an agent. They call the control plane API
(`/api/v1`, personal API tokens from goilerplate) and get live updates over SSE.

## Deploys

Docker Compose only. One code path for every app. No systemd mode: a single
binary is a compose file with one service.

What the repo has decides the input. The agent always gets a compose project.

| Repo has | ops does |
| --- | --- |
| `compose.yml` | uses it |
| only a `Dockerfile` | writes a one-service `compose.yml` |
| nothing (Go app) | writes a `Dockerfile` and the `compose.yml` |

The user's `compose.yml` stays untouched. ops writes `compose.ops.yml` next to
it with the Traefik labels, the shared network and the database mount.

1. `ops deploy` packs the working tree as a source tarball.
2. It uploads it to the control plane, which stores it in S3. No registry.
3. The control plane sends `deploy.start` with a presigned download URL.
4. The agent downloads, checks the SHA-256, builds with `docker compose build`, takes a snapshot backup, runs `docker compose up -d` and the health check.
5. Health check fails: the agent starts the previous images again and reports `deploy.failed`.
6. The agent keeps the images of the last 5 deploys for `ops rollback`.

The install script installs Docker when it is missing.

Env vars are stored encrypted in the control plane, sent with `deploy.start`,
and written to a `0600` env file compose reads.

## Config as code

Optional. Without it, ops detects everything. With it, the repo is the truth.

- The config is an `x-ops` block in `compose.yml`. No second file and no YAML
  library: the agent reads it through `docker compose config --format json`.
  `ops init` detects the repo and writes `compose.yml` with `x-ops`.
- Fields: `app`, `server`, `service` (gets the traffic), `port`, `health`,
  `domains`, `database`.
- Detection without config: `Dockerfile`, `go.mod` and `EXPOSE` give the build
  and the port. SQLite files (header `SQLite format 3`) on the service's mounts
  give the database. The deploy result shows what was found.
- Settings from `x-ops` show read-only in web, CLI and TUI ("from compose.yml").
  One source per field.
- Secrets never in the repo: `ops env set`, stored encrypted in the control
  plane. The backup bucket is an account setting.
- One pipeline for every trigger: the agent always gets a source tarball and
  builds on the server. `ops deploy` uploads the working tree (git ls-files),
  a GitHub push webhook later fetches the tarball from GitHub. This replaces
  local builds and `docker save`: no cross-arch builds, no image uploads.
- Push to deploy today: a GitHub Action that runs `ops deploy` with `OPS_TOKEN`.

## Two ways to build

The same agent pipeline for both; only where the image comes from differs.

| Way | Who builds | Trigger |
| --- | --- | --- |
| Server builds (default) | the user's own server, from the source tarball | `ops deploy`, later a push through a GitHub App without any workflow |
| CI builds | GitHub Actions or any CI, pushing to a registry | `ops deploy --image ghcr.io/x/app:sha`, or `curl -X POST` on the app's secret deploy webhook |

An image deploy reuses the compose.yml of the live release and swaps the image
of the main service: pull, up, health check, rollback on failure. Registry
logins are stored sealed like env vars. Builds never run on the control plane:
it only stores tarballs and relays messages.

Cloudflare (tunnel, no open ports) and Tailscale (SSH only on the tailnet) are
optional and recommended: one token each, ops does the rest. Without them
Traefik needs 80 and 443.

## Data on disk

The app names its database file, for example `x-ops: {database: /data/app.db}`.
The agent asks `docker inspect` for the host path behind that mount, so bind
mounts and named volumes both work. New apps get a bind mount at
`/var/lib/ops/apps/<app>/data`. A database outside any mount fails the deploy:
it would be lost on the next one.

## Proxy

Traefik as one shared container, configured through the labels in
`compose.ops.yml`. Not part of the agent: an agent update never takes apps down.

## Routing

Everything a visitor's request meets lives in the proxy layer: Traefik, one
shared container per server. The agent writes the routers and middlewares as
labels into the release's compose file. Config comes from `x-ops` and from
web, CLI, TUI and MCP; secrets such as the protection password are stored
sealed in the control plane, never in the repo.

```yaml
x-ops:
  domains: [blog.de, www.blog.de]
  canonical: blog.de   # www.blog.de redirects to blog.de; or the other way
  protect: true        # basic auth in front of the app
```

HTTPS is always on; there is no switch for plain HTTP.

| Feature | Layer |
| --- | --- |
| HTTP → HTTPS | Always on. Traefik redirect per router. In Cloudflare mode at the Cloudflare edge ("Always Use HTTPS" through the API): behind the tunnel Traefik only sees HTTP and would loop. |
| www ↔ apex | Traefik redirectregex per app, toggled by `canonical`. |
| Password protection | Traefik basic auth per app, bcrypt hash from the control plane. Later: "ops users only" through forward auth to the control plane. |
| Preview URLs | Later, done once and right: `<app>-<id>.<preview domain>` on a separate domain ops owns (never the brand domain), on the Public Suffix List, through a tunnel per server on ops' Cloudflare account, with an abuse contact. Not Cloudflare Quick Tunnels (random URLs, "not for production") and not sslip.io (open ports, shared Let's Encrypt limits). |

Order: domains with Traefik and Let's Encrypt, then Cloudflare mode (no open
ports), then the GitHub App, then preview URLs.

## GitHub

A GitHub App connects repos, like Vercel or Coolify. Installing it happens on
github.com (CLI and TUI open the link); everything after works in web, CLI,
TUI and MCP: link a repo and branch to an app, deploy a branch or commit
(`ops deploy --ref main`, no local checkout needed), auto-deploy on push, and
commit statuses on GitHub. The control plane fetches the tarball from GitHub
and feeds the same deploy pipeline.

## Domains

The user buys a domain anywhere.

- Any registrar: ops shows the A record to set, checks DNS, Traefik gets the certificate.
- Cloudflare: with the user's API token ops creates the DNS record and the tunnel itself. Porkbun API later.

Push to main: a GitHub Action that runs `ops deploy`. Later.

## Backups

Both run inside the agent. No second process.

- **Continuous**: Litestream v0.5 embedded as a Go library replicates the
  database to the user's S3-compatible bucket. Restore to any point in time.
  Probed on 2026-10-31: separate app process writing, agent replicating,
  latest and point-in-time restore both passed the integrity check.
- **Snapshot**: `VACUUM INTO`, gzip, upload. Before every deploy and migration, and on demand.
- **Restore drill**: daily restore into a temp file, then `PRAGMA integrity_check`. The UI shows "verified by restore".
- **New target**: when the bucket or folder changes, the agent clears Litestream's local state, so the new target starts with a full snapshot.

The app must run SQLite in WAL mode. Only the agent runs Litestream for a database.

## Security

Goal: a server with no open port at all.

- **ops itself**: the agent dials out. Nothing listens for ops.
- **App traffic**: Cloudflare Tunnel is the recommended mode. The agent
  creates the tunnel and the DNS record with the user's Cloudflare API token
  and runs `cloudflared` as a container in front of Traefik, which also dials
  out. Without Cloudflare, Traefik needs 80 and 443.
- **Admin access**: Tailscale through `tsnet` inside the agent. SSH and the
  emergency direct mode only answer on the tailnet, port 22 closes. Staging
  apps can be tailnet-only.
- **Firewall**: the agent sets it. Inbound only what the chosen mode needs.

## Data

ops changes data, never the schema. The data page, `ops db`, `ops sql` and
the MCP tools browse tables, edit rows and run SQL on the live database;
reads open it read-only. Tables and columns change only through the app's
own migrations in its repo, run when it starts; ops shows the newest
migration of goose, golang-migrate, Prisma or Drizzle. One way, no drift
between the server and the repo.

## Slices

Each slice works end to end in web, TUI, CLI, API and MCP before the next
one starts.

1. **Connect**: done.
2. **Deploy**: done 2026-10-31, with domains, Traefik, CI deploys and the GitHub App.
3. **Backups**: done 2026-10-31. Litestream, snapshots before every change, restore, daily drills.
4. **Data**: done 2026-10-31. Browse, edit, SQL; the schema stays with the app's migrations.
5. **Security**: done 2026-10-31. Cloudflare Tunnel, SSH on Tailscale, firewall.
6. **Beta**: Stripe plans, name and domain, preview URLs on their own domain, the first real servers.

## License

Decided 2026-11-01, after research:

- AGPL-3.0 for all of ops: real open source, so it lists where self-hosters
  look, next to Apache-licensed Coolify and Dokploy. A hosted clone is
  legal but must publish all of its code.
- Contributions come in under Apache 2.0, signed off on every commit
  (`CONTRIBUTING.md`, the pull request template). That keeps one move open:
  if a paid hosted clone of ops gains traction, future versions can switch
  to FSL-1.1-ALv2, which forbids a competing hosted service and turns into
  Apache 2.0 after two years.
- The name and logo are outside the license (`TRADEMARKS.md`). No
  registration for now; register the name at the paid launch or at its
  first misuse.

## Renaming later

ops launches as "ops by goilerplate" at ops.goilerplate.com with the
command `ops`. When a distinct name comes, it is one clean breaking change:
afterwards nothing carries the old name, no aliases and no mixed paths.

- **One release moves everything.** The agent finds the old names on its
  server and moves itself: `/etc/ops/rules`, `/var/lib/ops`, the `ops-agent`
  service, the `ops-` containers and the nftables table, then connects to
  the new host. Nobody logs in to a server by hand.
- **A short window, about four weeks.** The old host keeps answering only so
  agents can still fetch that release: a WebSocket does not follow
  redirects, and an agent that loses its control plane cannot be updated.
  The web pages and docs redirect.
- **A migration guide** in the docs and the CHANGELOG: what changes, why (the
  project grew and has its own name), what users do themselves (the command
  in scripts and CI, the GitHub Action, `ops login` with the new URL), and
  what to do if a server did not move.
- **Then the old name is gone:** host, command, paths and code. The product
  name lives only in `internal/build.Name`, so the code change is small;
  the paths on servers move with the release above.
- GitHub redirects a renamed repo and its Action, which covers links in old
  posts.
