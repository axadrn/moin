---
title: Changelog
description: What shipped in moin, newest first.
---

## v1.0.0-beta.23

November 4, 2026

**In short:** the old name ops is gone: only `moin` and its names work now, so update scripts, CI and compose files that still say ops.

### Changed
- **`x-ops` stops a deploy**: a compose.yml with an `x-ops` block fails with a message to rename it to `x-moin`, instead of counting.
- **Instant address user**: the user in front of a protected instant address is `moin`, and `moin protect` asks for `moin` by default; it takes effect with the app's next deploy.
- **Rules file**: the agent reads only `/etc/moin/rules`. Move `/etc/ops/rules` there if you still have one.

### Removed
- **The `ops` command**: use `moin`. An agent still installed as ops no longer updates itself; install moin on that server again.
- **`OPS_*` variables**: use `MOIN_URL`, `MOIN_TOKEN`, `MOIN_STATE_DIR`, `MOIN_RULES`, `MOIN_IMAGE` and `MOIN_PULL`.
- **The old CLI config folder**: run `moin login` once if the CLI says you are not logged in.
- **Old downloads and image**: `/download/ops-*` and `ghcr.io/axadrn/ops-controlplane` get no new releases; use `/download/moin-*` and `ghcr.io/axadrn/moin`.
- **`X-Ops-Signature`**: webhooks carry only `X-Moin-Signature`.
- **The move from ops**: `update.sh` no longer copies `/etc/ops-controlplane`, and settings that point to `/run/ops-controlplane` need `/run/moin-controlplane`.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`; agents update themselves. CLI: `curl -fsSL https://moin.run/cli.sh | sh`.

## v1.0.0-beta.22

November 4, 2026

**In short:** every app in the catalog speaks moin now, so installs keep working after the old name goes on December 4.

### Fixed
- **Catalog after December 4**: the 47 apps still used the old `x-ops` block in their compose files and would have stopped installing then. They use `x-moin` now.
- **Messages**: the last hints that still said `ops deploy` say `moin deploy`.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves.

## v1.0.0-beta.21

November 4, 2026

**In short:** the site says everything moin does today: Postgres next to SQLite everywhere, a fair comparison with sources, and answers to the questions people ask now.

### Changed
- **Postgres everywhere it belongs**: the homepage, the comparison and the docs name Postgres next to SQLite, for backups to any second, the daily restore check and the data browser.
- **A fairer comparison**: a new row for Postgres backups, every competitor fact from their own docs.
- **Answers that are true today**: any language deploys without a Dockerfile, the catalog, instant addresses, `moin exec`, and alerts for backups, deploys and full disks.
- **Roadmap**: what shipped moved to Now; MySQL backups are next.
- **A new footer**: product, docs, open source and company, at a glance.
- **Why moin**: its own section on the homepage, linked under the buttons.

### Fixed
- **Self-hosting commands**: the install URL and `git clone` pointed at the old repository name.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`.

## v1.0.0-beta.20

November 4, 2026

**In short:** ops is now moin, at moin.run. Deploy anything. Lose nothing. Everything you run moves over by itself, and nothing breaks on the way.

### Why
moin is how Hamburg says hello. Hamburg is a container port, and your apps run in containers too. [The story](https://moin.run/#why-moin).

### Changed
- **New name, new home**: moin at [moin.run](https://moin.run), the command `moin`, the logo `moin.`. The old address sends you to the new one.
- **Your servers move by themselves**: the next agent update renames everything on the server to moin, copies each app's data and checks it file by file. Apps keep serving while it moves.
- **Backups go on**: Postgres starts a fresh continuous backup under its new name right away. Every older snapshot and point in time still restores.
- **Self-hosted moves too**: `update.sh` copies your settings and data to the new folders and starts moin from the image `ghcr.io/axadrn/moin`. The old folders stay.
- **Old names keep working until December 4, 2026**: the `ops` command, `x-ops` in compose.yml, `OPS_*` variables, the old download paths and image, and the `X-Ops-Signature` webhook header next to the new `X-Moin-Signature`.
- **One address for everything**: write to hello@moin.run.

### Upgrade
Hosted: nothing to do. Self-hosted: **Update now**, or `sh update.sh`; agents follow by themselves. CLI: `curl -fsSL https://moin.run/cli.sh | sh`.

## v1.0.0-beta.19

November 4, 2026

**In short:** 47 apps in the catalog, with AI on your own server: Open WebUI, Ollama, Flowise, plus Metabase, Outline, Twenty, Directus, Jellyfin and more.

### New
- **17 more apps**: Open WebUI, Ollama, Flowise, Activepieces, Metabase, Outline, Twenty, Directus, Appsmith, Jellyfin, Audiobookshelf, Trilium, Homepage, Gatus, Healthchecks, PrivateBin and Seafile. Each one installed for real before it got in.
- **An AI category**: chat with your own models, next to your data.

### Fixed
- **Newest releases from GitHub's registry**: ops read only the first page of tags, so some apps showed an old release as the newest. It reads them all now.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. The catalog comes with it.

## v1.0.0-beta.18

November 4, 2026

**In short:** run a command in your app's container from anywhere, no SSH: migrations, the first admin, a quick look.

### New
- **Run a command**: `ops exec shop -- rails db:migrate`, or `--service db -- psql -c "…"`. The output streams live and the command's exit code comes back. Web: **Run a command** on the app page. Also the TUI `:exec`, the API and MCP `exec_command`.
- **Safe by default**: only owners and admins, never through a shell, at most an hour, every run in the activity with its exit code. `exec: off` in `/etc/ops/rules` turns it off for a server.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves; an older agent answers that it needs the update first.

## v1.0.0-beta.17

November 4, 2026

**In short:** every app is online at once, with HTTPS and a password, before it has a domain; and each service of an app can have its own.

### New
- **Instant address**: after its first deploy every app answers at `<app>.<your-server-ip>.sslip.io` with HTTPS, guarded by a password until you open it. Switch to `nip.io` if one is ever down. Web: **Instant address** on the app page. CLI: `ops instant`, `ops instant nip.io`, `ops instant open`.
- **Domains per service**: `shop.com` for `web` and `api.shop.com` for `api` in one app. In compose.yml under `x-ops.domains`, or `ops domains add --service api api.shop.com`.
- **Uptime for every service**: each domain of each service is checked every minute.

### Changed
- **`ops deploy`** ends with the address the app is live at.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves; servers behind a Cloudflare Tunnel get no instant address, since it needs ports 80 and 443 open.

## v1.0.0-beta.16

November 4, 2026

**In short:** one page tells you whether everything is OK, and ⌘K takes you anywhere.

### New
- **Overview**: the first page of a workspace. "All good." when it is, otherwise what needs you: a server offline, a failed deploy, a backup that stopped, a domain down, a full disk. Each with the way to fix it.
- **`ops status`**: the same in your terminal, and it exits 1 when something needs you, for scripts and CI. Also the TUI `:status`, the API and MCP `get_status`.
- **⌘K**: jump to any app, server, bucket or page, and deploy, restart or roll back from the keyboard. `?` shows every shortcut.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves.

## v1.0.0-beta.15

November 4, 2026

**In short:** 30 open-source apps on your server in one click, any version, updates with a backup first: Umami, n8n, Grafana, Nextcloud, Vaultwarden, WordPress and more.

### New
- **App catalog**: browse at `/apps`, install from the Apps page, `ops catalog install umami --server web-1`, the TUI, the API or MCP. ops makes the secrets, sets HTTPS on your domain and backs up its Postgres or SQLite.
- **Every version**: pick any release, newest first with its date, fetched from Docker Hub and GitHub once an hour.
- **Updates**: "Umami 3.5.0 is out" on the app page, `ops update <app>`, with a snapshot first. An `update.available` alert tells you once per release.
- **RSS for the changelog**: `/changelog.xml`, and a dot in the account menu when there is something new.

### Changed
- **A calmer navbar**: Pricing, Docs, Changelog, and Apps on its own. Roadmap moved to the footer, and Get started is the one way in.
- **The app sidebar**: no app list and no shortcut letters; Docs, Support and the changelog are in the account menu.
- **Health checks**: a redirect counts as up, and a slow first start gets two minutes.

### Fixed
- **Docs on a phone**: a long page title no longer runs under the search.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves.

## v1.0.0-beta.14

November 4, 2026

**In short:** self-host ops on one server with one command, and the home page shows in one table how fresh your backups are.

### New
- **One server, one command**: `curl -fsSL https://raw.githubusercontent.com/axadrn/ops/main/controlplane/deploy/install.sh | sh -s -- --domain ops.example.com --email you@example.com` installs Docker, the control plane and the agent, and serves it on your domain with a Let's Encrypt certificate. It prints your sign-in link.
- **Three ways to run ops**: hosted, one server, or two servers. The [self-hosting guide](/docs/self-hosting) says when to pick which.

### Changed
- **Self-hosted without email or a bucket**: a self-hosted control plane starts without them. Sign-in links go to its log, and `docker compose -f /opt/ops/compose.yml exec controlplane ./main sign-in-link` prints one. Add email and a bucket any time.

### Fixed
- **Apps that wait for their database**: a service that restarts once while its database starts no longer fails the deploy. ops now waits until it has run for 10 seconds without a restart, up to 30 seconds.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Nothing changes for a control plane on its own server.

## v1.0.0-beta.13

November 4, 2026

**In short:** you hear it when a backup stops, a restore check fails or a disk fills up, and servers clean up old images by themselves.

### New
- **Backup alerts**: a backup that stops working, or a daily restore check that fails, sends an alert by email, Slack, Discord, Telegram or webhook, once, and again when it works. `ops alerts add …`, events `backup.failed`, `drill.failed`.
- **Disk alerts**: a server over 90% full sends an alert, and another when it is back under 85%. Event `disk.full`.
- **Disk cleanup**: after every deploy and once a day, servers remove images no release needs anymore, dangling images and build cache older than a week. Rollback images and volumes stay.

### Fixed
- **Restore check while a database is down**: it no longer fails just because the Postgres service is stopped.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves. Destinations that get every event also get the new ones.

## v1.0.0-beta.12

November 4, 2026

**In short:** deploy any app without a Dockerfile: ops reads your repo and builds Node, Bun, Python, Ruby, PHP and more.

### New
- **No Dockerfile needed**: a repo without `compose.yml`, Dockerfile or Go module now builds with Railpack. ops finds the language, its version and the start command, and runs it on `PORT` 8080. The deploy log says what it found, such as `Railpack: Node 22, start: npm run start`.
- **Build secrets stay secret**: your env vars reach the build as secrets, never in the image.
- **Start command of your own**: set `RAILPACK_START_CMD` with `ops env set`.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves.

## v1.0.0-beta.11

November 4, 2026

**In short:** `${VAR}` in compose.yml takes your env vars from ops, so a Postgres service gets its password; update if your compose uses variables.

### Fixed
- **Variables in compose.yml**: `${DB_PASSWORD}` and the like now take the app's env vars from ops, as a `.env` next to the file would. Before, they came out empty and a service such as Postgres did not start.
- **Deploys while a database is down**: a Postgres that does not run no longer blocks the deploy that could fix it. The deploy log says there was no snapshot, and why.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves; then deploy the app again.

## v1.0.0-beta.10

November 4, 2026

**In short:** Postgres gets what SQLite has: every change backed up, full copies, a daily restore check and the data browser. And a new home page: Deploy anything. Lose nothing.

### New
- **Postgres backups**: every change goes to your bucket within a minute, seconds when it's busy, with WAL-G. Go back to any second of the last 7 days: `ops restore --at "2026-11-04 14:02"`.
- **Postgres snapshots**: a full copy every hour and before every deploy, restore and edit. `ops backup`, `ops restore <snapshot>`.
- **Checked every day**: ops restores the newest Postgres copy into a throwaway database and checks it, like SQLite.
- **Postgres in the data browser**: tables, rows, schema, search, edit and delete by primary key, and read-only SQL. Web: the **Data** tab. CLI: `ops db`, `ops sql`.
- **Rows by primary key**: SQLite tables without a rowid can be edited too.

### Changed
- **Home page**: "Deploy anything. Lose nothing. Open source." The product shots change by themselves.
- **Reads stay reads**: SQL that changes data is refused up front in a read, with a hint to use `--write`.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves; deploy an app once so ops finds its Postgres.

## v1.0.0-beta.9

November 4, 2026

**In short:** a new domain on a server without a Cloudflare Tunnel gets its HTTPS certificate again; update if you add domains.

### Fixed
- **HTTPS for new domains**: since beta.1, a domain added on a server that serves ports 80 and 443 itself got no Let's Encrypt certificate and showed a browser warning. It gets one again on the next deploy. Servers behind a Cloudflare Tunnel were never affected.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves; then deploy each app with a new domain once.

## v1.0.0-beta.8

November 4, 2026

**In short:** the app page puts Deploy from GitHub first; deploying from CI waits behind one click.

### Changed
- **Deploy from CI**: deploying an image and the deploy hook now sit folded under **Deploy from CI** on the app page, so the GitHub **Deploy** is the one you see. An app with a hook shows it open.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves.

## v1.0.0-beta.7

November 4, 2026

**In short:** apps and servers are slim cards you drag as a whole, the version of ops is always in sight, and changing repos on github.com no longer ends on an error.

### New
- **Version in the sidebar**: the control plane's version sits at the bottom of the sidebar and opens the changelog.
- **Update pending**: a server whose agent is behind says so next to its version; it updates itself on its next connect.

### Changed
- **Apps and servers lists**: every app and server is a slim card of its own, with its grip on the left, the same on both pages.
- **List and grid switch**: icons only.

### Fixed
- **GitHub repos changed on github.com**: coming back to ops no longer shows "GitHub is not connected"; the change was saved all along.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves.

## v1.0.0-beta.6

November 4, 2026

**In short:** every agent now updates itself, so metrics arrive everywhere, charts move to a file of their own to keep the database small, and the app tabs fit on a phone.

### Changed
- **Charts in their own file**: metrics and hourly uptime live in `history.db` next to the database, without a backup. The database stays small and a restore fast. The charts start over once; outages and uptime state stay.
- **Home page**: the live numbers show from 1,000 deploys on.

### Fixed
- **Agents that never updated**: an agent built from a commit instead of a release, such as one installed from `main`, now updates itself like any older agent.
- **Metrics page**: no longer tells you to update agents by hand; they update themselves.
- **App tabs on a phone**: Overview, Data, Backups, Uptime and Metrics no longer push the page wider than the screen; swipe them.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves.

## v1.0.0-beta.5

November 4, 2026

**In short:** alerts go to Slack, Discord, Telegram and webhooks, every server and app has live charts, and each app can back up to its own bucket.

### New
- **Alerts where your team is**: send alerts to Slack, Discord, Telegram or a signed webhook, as well as email, with a test for each. Web: **Alerts**. CLI: `ops alerts add slack <name>`. Also the TUI settings, API and MCP.
- **Live metrics**: CPU, memory, disk and network of every server and app, live for the last hour and back 30 days, with each deploy marked. Web: the **Metrics** tab. CLI: `ops metrics shop`. TUI: `m`. API and MCP too.
- **More than one bucket**: give each app its own bucket for uploads and backups, say one per client. Web: **Buckets**. CLI: `ops buckets add`, `ops buckets use`.

### Changed
- **Buckets**: the bucket you had is now the default one. Older backups stay where they were and restore from there when an app moves.
- **Request and error counts** per app are not in the metrics yet; response times are on the Uptime tab.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Agents update themselves; until then, metrics and per-app buckets wait for the new agent.

## v1.0.0-beta.4

November 3, 2026

**In short:** ops watches your sites every minute and mails you when one goes down, puts apps and servers in your order, and updates itself and its agents.

### New
- **Uptime checks, built in**: every domain of every app is checked from outside once a minute, from the first deploy on, with nothing to set up. The app's **Uptime** tab shows 24 hours, 7 and 30 days, the certificate and the last outages. Also `ops uptime`, the TUI (`u` on an app), API and MCP.
- **Your own order**: drag apps and servers where you want them, or `ops apps move blog top`, `K` / `J` in the TUI. Everyone in the workspace sees the same order everywhere.
- **List or grid**: switch how the Apps and Servers pages look; ops remembers it for you.
- **Update now**: a self-hosted ops shows when a release is out, with its notes, and updates in one click. If the new version does not start, the old one comes back. Also `ops update`.
- **Agents update themselves** to the control plane's version, one server at a time and never during a deploy. Turn it off with `ops servers auto-update off`.
- **Email alerts**: owners and admins get a mail when a site goes down or comes back, a server goes offline or a deploy fails. Turn it off for you with `ops alerts off`.

### Changed
- **Activity**: a server going offline and coming back now shows up there, and so does every site that goes down and comes back.
- **Self-hosted gets releases only**: `update.sh` takes the newest release, not every build in between.
- **A copy of the database before every upgrade**: ops writes `backup-before-<version>.db` next to it before it changes the schema.

### Fixed
- **Signing in with GitHub finds your account** by any verified email on your GitHub account, not only the primary one, instead of making a new empty account.

### Upgrade
Self-hosted: `git pull` in the repo, then `sh update.sh` once, which also starts the updater. From then on: **Update now**.

## v1.0.0-beta.3

November 3, 2026

**In short:** Vim keys in the terminal, env vars as one `.env` file, and a dead server can finally go.

### New
- **Vim keys in the TUI**: `j` `k`, `gg` `G`, `/` to search, `:` for commands, `?` for help. Logs scroll, and `G` follows the end again.
- **Env vars as one `.env` text**: paste or edit them all at once. Web: **Fields | Text**. CLI: `ops env push`. TUI: `e`, then `edit`. API `PUT /api/v1/apps/{app}/env`, MCP `replace_env`. ops shows what changes and asks first.

### Changed
- **TUI keys moved** to make room for vim: GitHub `g` → `gh`, logs `l` → `L`, rename `n` → `cw`, deploy hook `h` / `H` → `:hook` / `:hook off`.
- **Admin is only on the hosted control plane**: self-hosted, you manage people under **Team**; `ADMIN_EMAILS` only says who may sign up.
- **Free forever is what you tick** (hosted Admin): Admin shows each user's plan (trial, trial ended, free forever, paying) and turns free forever on or off.

### Fixed
- **A dead server can be removed**: offline, it goes with its apps, which stay on the machine if it comes back. Online, its apps still go first.

### Removed
- **The waitlist**: sign-up is open. The first start deletes the `waitlist` table with its addresses; whoever is in keeps their plan.

### Upgrade
Self-hosted: pull the new image and restart. Agents need no update.

## v1.0.0-beta.2

November 2, 2026

**In short:** Sign in with GitHub in one click, and the first page asks only your name and your workspace.

### New
- **Sign in with GitHub first**: "Continue with GitHub" is the main way in; the email link stays below it, and a password is optional in the settings.
- **One short first page**: your name and your workspace name. From GitHub both are filled in, so it's one click. Invited people only give their name.
- **API**: `POST /api/v1/auth/onboarding` takes an optional `workspace_name`.

### Fixed
- **"Continue with GitHub" only shows when GitHub sign-in is set up**, so it never leads to a GitHub error page.

### Upgrade
Self-hosted: pull the new image and restart. For GitHub sign-in set `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET`: a GitHub App's client ID and secret work, with "Email addresses: read-only" and `<your url>/auth/github/callback` as a redirect URI.

## v1.0.0-beta.1

November 2, 2026

**In short:** ops is out, as a paid beta: deploy your apps to your own servers, and keep every change of their SQLite database in your own bucket. Hosted is $29 a month or $249 a year per workspace, 30 days free, no card.

### New
- **Deploy**: from your repo, a GitHub branch or commit, or an image from CI. Every push to GitHub can deploy by itself, and the commit shows the deploy status. A failed deploy brings the last good one back.
- **Backups**: every SQLite change goes into your bucket, a snapshot before every change ops makes, restore to any second, and a restore check every day.
- **Data**: browse tables, edit rows and run SQL on the live database.
- **Domains**: HTTPS, a canonical domain, and a password in front if you want one.
- **Security**: Cloudflare Tunnel instead of open ports, SSH only over Tailscale, a firewall, and a rules file on your server. The agent dials out; ops holds no key to your servers.
- **Teams and the activity log**: roles, invitations, and who changed what, to the second.
- **Every surface**: web, CLI, terminal UI, API and MCP.
- **Self-hosting**: every feature, free, under AGPL.

### Upgrade
New install: see [getting started](https://ops.goilerplate.com/docs/getting-started). What comes next is on the [roadmap](https://ops.goilerplate.com/docs/roadmap).

