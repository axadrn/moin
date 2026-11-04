---
title: Changelog
description: What shipped in ops, newest first.
---

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

