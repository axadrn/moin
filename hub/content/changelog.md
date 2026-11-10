---
title: Changelog
description: What shipped in moin, newest first.
---

## v1.0.0-beta.27

November 10, 2026

**In short:** moin has a new face. Our own UI. No Tailwind. Yours to copy.

### New
- **moin UI**: Our own UI kit. Native HTML, CSS and JS. No build step. Copy it into your project. See `/ui`.
- **moin next**: What grows next to deploys. Highly experimental. See `/next`.
- **A real roadmap**: Now. Soon. Later. All on one page at `/roadmap`.
- **Search finds moin UI**: Components and blocks show up in the site search.

### Changed
- **Every page is new**: Rebuilt on moin UI. Softer corners. No more pills. One look everywhere.
- **The home page reads faster**: Fewer sections. One bento for what moin does.
- **The changelog is a timeline**: Each version in one line. The notes one click away.

### Fixed
- **Switches tell the truth**: A change that is refused or fails goes back to what is saved. The back button shows the real state too. (#49)

### Upgrade

Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`.

## v1.0.0-beta.26

November 9, 2026

**In short:** A new web app and simple backups; stop apps that use SQLite before you update.

### New
- **A new web app**: Grouped lists, one row of tabs per app, and pages that load whole and fast. Light and dark. Built from moin's own parts.
- **One search everywhere**: Press `⌘K` or `/`. The same box opens in the web and in the terminal UI.
- **Backups on your schedule**: moin copies every SQLite and Postgres database to your own bucket. Pick every 15 minutes, hourly or daily. Keep each copy for 1, 3, 7 or 30 days.
- **A daily check**: moin restores the newest copy into a throwaway database and reads all of it.
- **Undo a restore**: A restore keeps the database as it was. One step puts it back.
- **Download a copy**: Get the full database as one file. On the web, with `moin backup download`, or in the terminal UI.
- **New databases are found by themselves**: moin looks again every hour. A SQLite file created long after a deploy gets backed up too.
- **Red when moin can't back something up**: MySQL, MongoDB and a Postgres in an unknown image show in red. They no longer look safe.
- **`backups` in x-moin**: Set `every` and `keep` in `compose.yml`.
- **What happens when**: A new page says what moin does when a server restarts, a bucket is gone or a copy is broken. We tried each case on real servers.
- **Rename an app**: Do it in the app's Settings, with `moin apps rename` or through the API. Its data and backups stay. (#17)
- **Short links**: Links are plain names you can read, like `/app/acme/apps/my-app`. Links inside moin follow a rename. An old bookmark shows "not found".
- **Compare moin**: Each page at `/compare/<name>` shows moin next to Coolify, Dokploy, Kamal, Railway, Render, Fly.io or Heroku, fact by fact.
- **Charts of your own**: Server and app metrics, drawn by the server.
- **The catalog has its own place**: Find it at `/catalog`. Each app shows its logo, its source and its versions.
- **Pin apps**: Pinned apps come first.

### Changed
- **The agent is now the daemon, and the control plane the hub**: The new names are in every command, page and file. An update moves a server over by itself.
- **`moin deploy` asks before it creates an app**: A typo no longer makes a new app. Scripts pass `--yes`.
- **A setting in compose.yml is locked in moin**: Domains, backups, the instant address and a required password show **from compose.yml**. Change them only there.
- **Saving never restarts an app**: A changed env var, domain or password waits. The top of the app page says how many wait.
- **Restores never change your data in place**: moin builds the restored database next to the live one. Then it switches in one step.
- **A big database gets the time it needs**: A copy runs as long as data moves.
- **A database over 1 GB gets no extra copy before each data edit**: The newest scheduled copy is the way back. Deploys still take a copy first.
- **One word per thing**: The web, CLI, terminal UI, API and MCP say the same words. Old names are gone, with no aliases and no redirects.
- **The docs**: Short pages in plain words, and one overview of every way to deploy.

### Fixed
- **A bucket that is gone**: The backup list says so within seconds. `moin deploy` names the bucket, not the hub.
- **A restart in the middle of a deploy**: The app comes back by itself. The deploy ends as soon as the server is back. It no longer blocks the app for 30 minutes.
- **A failed daily check**: The backup shows red everywhere, not green next to a broken copy.
- **A full disk no longer starts a restore**: moin checks the room against the size noted on the copy.
- **A lost connection during a restore**: moin says it cannot tell. It no longer says "nothing changed".
- **A broken bucket**: It pauses only its own app, not the others on the server.
- **Backup alerts**: A stopped app no longer raises them.

### Removed
- **Every change for SQLite**: Going back to any minute is gone. It could damage the live database of apps that open SQLite for each request. Pick every 15 minutes for the closest backups.
- **`x-moin.database`**: Every database is backed up anyway. Pick one in the list or with `--db`.
- **Aliases in the CLI**: `ls`, `list`, `remove`, `unset`, `moin app` and `registry add` are gone. Use the one name each command has now.
- **`moin agent`**: Use `moin daemon`.

### Upgrade
1. Stop every app that uses SQLite: `moin stop`. The old backup process can damage a database that is being written when it exits.
2. Self-hosted: update with **Update now**, or `sh update.sh`. Your servers follow by themselves.
3. Self-hosted with a Cloudflare Tunnel: right after the update, change its service from `http://controlplane:8090` to `http://hub:8090`.
4. Self-hosted with a path under `/run/moin-controlplane/` in 1Password: change it to `/run/moin-hub/`.
5. Start your apps again: `moin start`.
6. CLI: `curl -fsSL https://moin.run/cli.sh | sh`.

Old copies stay in your bucket. moin no longer lists them. Do not delete them.

## v1.0.0-beta.25

November 5, 2026

**In short:** A new terminal UI, `moin login` through the browser and a new home page.

### New
- **A new terminal UI**: `moin` opens a dashboard of your apps and servers. `space` shows everything you can do. `space space` searches apps, servers and actions. `esc` goes back.
- **Instant start**: The terminal UI shows your last view at once and refreshes behind it. Switches and env changes show before the server answers.
- **Log in through the browser**: `moin login` and the terminal UI show a code. Approve it in the browser with GitHub, an email link or your password. No token to copy.
- **Your computers under Tokens**: Each browser login shows as "CLI on <computer name>". It ends after 90 days without use. You can sign each one out.
- **Where things work**: A docs page lists every feature for web, CLI, terminal UI, API and MCP. Where one is missing, it says why.
- **A new home page**: One calm screen with a wall of app containers. Then everything moin does in tabs: live in two minutes, security, backups, four ways in and a comparison.
- **Built in the open**: The home page shows how often moin ships, read from the changelog.

### Changed
- **One promise everywhere**: Deploy anything. Lose nothing. Your apps on your servers. Every change backed up. Every restore tested. Home, sign-in, docs, CLI and catalog say the same.
- **Deploy now**: The main button says Deploy now everywhere. It used to say Get started or Start free.
- **Calmer navbar**: On a phone, only the logo and the menu show. Apps has no New badge. GitHub stars show from 1,000 on.
- **Comparison in black and white**: Color only means status now.
- **`moin login` in a terminal**: It always goes through the browser. To pass a token, use `--token`, stdin or `MOIN_TOKEN`.
- **Setup in the web**: The terminal UI covers daily work. Team, billing, adding servers, GitHub, the catalog and SQL open the right page in the browser.

### Fixed
- **http on your tailnet**: The CLI connects over plain http to short tailnet names like your server's name, not only to IP addresses.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`.

## v1.0.0-beta.24

November 5, 2026

**In short:** The old name ops is gone and only `moin` works now, so update scripts, CI and compose files that still say ops.

### Changed
- **Guides**: A small book icon on each card opens its guide. It replaces the "How it works" link.
- **`x-ops` stops a deploy**: A compose.yml with an `x-ops` block now fails. The message tells you to rename it to `x-moin`.
- **Instant address user**: The user in front of a protected instant address is `moin`. `moin protect` asks for `moin` by default. It takes effect with the app's next deploy.
- **Rules file**: moin reads only `/etc/moin/rules`. Move `/etc/ops/rules` there if you still have one.

### Fixed
- **Switches stay where you put them**: Every switch saves only its own setting and shows what was saved. "Deploy every push" no longer flips back when GitHub is slow.
- **Uptime pause sticks**: A paused check stays paused, even when a check was running at that moment or the hub restarts.

### Removed
- **The `ops` command**: Use `moin`. A server still running ops no longer updates itself. Install moin on that server again.
- **`OPS_*` variables**: Use `MOIN_URL`, `MOIN_TOKEN`, `MOIN_STATE_DIR`, `MOIN_RULES`, `MOIN_IMAGE` and `MOIN_PULL`.
- **The old CLI config folder**: Run `moin login` once if the CLI says you are not logged in.
- **Old downloads and image**: `/download/ops-*` and `ghcr.io/axadrn/ops-controlplane` get no new releases. Use `/download/moin-*` and `ghcr.io/axadrn/moin`.
- **`X-Ops-Signature`**: Webhooks carry only `X-Moin-Signature`.
- **The move from ops**: `update.sh` no longer copies `/etc/ops-controlplane`. Settings that point to `/run/ops-controlplane` need `/run/moin-controlplane`.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves. CLI: `curl -fsSL https://moin.run/cli.sh | sh`.

## v1.0.0-beta.23

November 4, 2026

**In short:** Page counts only on the public site, never inside your workspace.

### Changed
- **Analytics only where it belongs**: Only the home page, docs, changelog, catalog and legal pages are counted. The app, the admin and sign-in never are. No workspace name, app name or sign-in link reaches a page counter.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`.

## v1.0.0-beta.22

November 4, 2026

**In short:** Every app in the catalog now uses the moin name, so installs keep working after the old name goes on December 4.

### Fixed
- **Catalog after December 4**: The 47 apps still used the old `x-ops` block in their compose files. They would have stopped installing then. They use `x-moin` now.
- **Messages**: The last hints that still said `ops deploy` now say `moin deploy`.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves.

## v1.0.0-beta.21

November 4, 2026

**In short:** The site says everything moin does today: Postgres next to SQLite, a fair comparison with sources, and answers to the questions people ask now.

### Changed
- **Postgres everywhere it belongs**: The homepage, the comparison and the docs name Postgres next to SQLite. That covers backups to any second, the daily restore check and the data browser.
- **A fairer comparison**: A new row for Postgres backups. Every competitor fact comes from their own docs.
- **Answers that are true today**: They now cover any language without a Dockerfile, the catalog, instant addresses, `moin exec`, and alerts for backups, deploys and full disks.
- **Roadmap**: What shipped moved to Now. MySQL backups are next.
- **A new footer**: Product, docs, open source and company, at a glance.
- **Why moin**: It has its own section on the homepage, linked under the buttons.

### Fixed
- **Self-hosting commands**: The install URL and `git clone` pointed at the old repository name. They use the right one now.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`.

## v1.0.0-beta.20

November 4, 2026

**In short:** ops is now moin, at moin.run; everything you run moves over by itself, and nothing breaks on the way.

### Why
moin is how Hamburg says hello. Hamburg is a container port, and your apps run in containers too. [The story](https://moin.run/#why-moin).

### Changed
- **New name, new home**: moin lives at [moin.run](https://moin.run). The command is `moin`. The logo is `moin.`. The old address sends you to the new one.
- **Your servers move by themselves**: The next update renames everything on the server to moin. It copies each app's data and checks it file by file. Apps keep serving while it moves.
- **Backups go on**: Postgres starts a fresh continuous backup under its new name right away. Every older snapshot and point in time still restores.
- **Self-hosted moves too**: `update.sh` copies your settings and data to the new folders. It starts moin from the image `ghcr.io/axadrn/moin`. The old folders stay.
- **Old names keep working until December 4, 2026**: The `ops` command, `x-ops` in compose.yml, `OPS_*` variables, the old download paths and image, and the `X-Ops-Signature` webhook header next to the new `X-Moin-Signature`.
- **One address for everything**: Write to hello@moin.run.

### Upgrade
Hosted: nothing to do. Self-hosted: **Update now**, or `sh update.sh`. Your servers follow by themselves. CLI: `curl -fsSL https://moin.run/cli.sh | sh`.

## v1.0.0-beta.19

November 4, 2026

**In short:** 47 apps in the catalog, with AI on your own server: Open WebUI, Ollama, Flowise, plus Metabase, Outline, Twenty, Directus, Jellyfin and more.

### New
- **17 more apps**: Open WebUI, Ollama, Flowise, Activepieces, Metabase, Outline, Twenty, Directus, Appsmith, Jellyfin, Audiobookshelf, Trilium, Homepage, Gatus, Healthchecks, PrivateBin and Seafile. Each one was installed for real before it got in.
- **An AI category**: Chat with your own models, next to your data.

### Fixed
- **Newest releases from GitHub's registry**: ops read only the first page of tags. Some apps showed an old release as the newest. It reads them all now.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. The catalog comes with it.

## v1.0.0-beta.18

November 4, 2026

**In short:** Run a command in your app's container from anywhere, with no SSH: migrations, the first admin, a quick look.

### New
- **Run a command**: `ops exec shop -- rails db:migrate`, or `--service db -- psql -c "…"`. The output streams live. The command's exit code comes back. Web: **Run a command** on the app page. Also the TUI `:exec`, the API and MCP `exec_command`.
- **Safe by default**: Only owners and admins can run a command. It never goes through a shell. It runs at most an hour. Every run shows in the activity with its exit code. `exec: off` in `/etc/ops/rules` turns it off for a server.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves. A server that has not updated yet says it needs the update first.

## v1.0.0-beta.17

November 4, 2026

**In short:** Every app is online at once, with HTTPS and a password, before it has a domain; each service of an app can have its own domain too.

### New
- **Instant address**: After its first deploy, every app answers at `<app>.<your-server-ip>.sslip.io` with HTTPS. A password guards it until you open it. Switch to `nip.io` if one is ever down. Web: **Instant address** on the app page. CLI: `ops instant`, `ops instant nip.io`, `ops instant open`. (#39)
- **Domains per service**: `shop.com` for `web` and `api.shop.com` for `api`, in one app. Set it in compose.yml under `x-ops.domains`, or with `ops domains add --service api api.shop.com`. (#32)
- **Uptime for every service**: Each domain of each service is checked every minute. (#32)

### Changed
- **`ops deploy`**: It ends with the address the app is live at. (#39)

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves. A server behind a Cloudflare Tunnel gets no instant address, because it needs ports 80 and 443 open.

## v1.0.0-beta.16

November 4, 2026

**In short:** One page tells you whether everything is OK, and ⌘K takes you anywhere.

### New
- **Overview**: The first page of a workspace. It says "All good." when it is. If not, it shows what needs you: a server offline, a failed deploy, a backup that stopped, a domain down, a full disk. Each comes with the way to fix it. (#43)
- **`ops status`**: The same in your terminal. It exits 1 when something needs you, for scripts and CI. Also the TUI `:status`, the API and MCP `get_status`. (#43)
- **⌘K**: Jump to any app, server, bucket or page. Deploy, restart or roll back from the keyboard. `?` shows every shortcut. (#44)

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves.

## v1.0.0-beta.15

November 4, 2026

**In short:** 30 open-source apps on your server in one click, any version, updates with a backup first: Umami, n8n, Grafana, Nextcloud, Vaultwarden, WordPress and more.

### New
- **App catalog**: Browse at `/apps`. Install from the Apps page, with `ops catalog install umami --server web-1`, the TUI, the API or MCP. ops makes the secrets, sets HTTPS on your domain and backs up its Postgres or SQLite.
- **Every version**: Pick any release, newest first with its date. ops fetches them from Docker Hub and GitHub once an hour.
- **Updates**: "Umami 3.5.0 is out" shows on the app page. Run `ops update <app>`. A snapshot comes first. An `update.available` alert tells you once per release.
- **RSS for the changelog**: `/changelog.xml`, and a dot in the account menu when there is something new.

### Changed
- **A calmer navbar**: Pricing, Docs, Changelog, and Apps on its own. Roadmap moved to the footer. Get started is the one way in.
- **The app sidebar**: No app list and no shortcut letters. Docs, Support and the changelog are in the account menu.
- **Health checks**: A redirect counts as up. A slow first start gets two minutes.

### Fixed
- **Docs on a phone**: A long page title no longer runs under the search.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves.

## v1.0.0-beta.14

November 4, 2026

**In short:** Self-host ops on one server with one command, and see in one table on the home page how fresh your backups are.

### New
- **One server, one command**: `curl -fsSL https://raw.githubusercontent.com/axadrn/ops/main/controlplane/deploy/install.sh | sh -s -- --domain ops.example.com --email you@example.com` installs Docker, the hub and the daemon. It serves ops on your domain with a Let's Encrypt certificate. It prints your sign-in link.
- **Three ways to run ops**: Hosted, one server, or two servers. The [self-hosting guide](/docs/self-hosting) says when to pick which.

### Changed
- **Self-hosted without email or a bucket**: A self-hosted hub starts without them. Sign-in links go to its log. `docker compose -f /opt/ops/compose.yml exec controlplane ./main sign-in-link` prints one. Add email and a bucket any time.

### Fixed
- **Apps that wait for their database**: A service that restarts once while its database starts no longer fails the deploy. ops now waits until it has run for 10 seconds without a restart, up to 30 seconds.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Nothing changes for a hub on its own server.

## v1.0.0-beta.13

November 4, 2026

**In short:** You hear it when a backup stops, a restore check fails or a disk fills up, and servers clean up old images by themselves.

### New
- **Backup alerts**: A backup that stops working, or a daily restore check that fails, sends an alert by email, Slack, Discord, Telegram or webhook. It sends once, and again when it works. `ops alerts add …`, events `backup.failed`, `drill.failed`.
- **Disk alerts**: A server over 90% full sends an alert. Another comes when it is back under 85%. Event `disk.full`.
- **Disk cleanup**: After every deploy and once a day, servers remove images no release needs anymore, dangling images and build cache older than a week. Rollback images and volumes stay.

### Fixed
- **Restore check while a database is down**: It no longer fails just because the Postgres service is stopped.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves. Destinations that get every event also get the new ones.

## v1.0.0-beta.12

November 4, 2026

**In short:** Deploy any app without a Dockerfile: ops reads your repo and builds Node, Bun, Python, Ruby, PHP and more.

### New
- **No Dockerfile needed**: A repo without `compose.yml`, Dockerfile or Go module now builds with Railpack. ops finds the language, its version and the start command. It runs the app on `PORT` 8080. The deploy log says what it found, such as `Railpack: Node 22, start: npm run start`.
- **Build secrets stay secret**: Your env vars reach the build as secrets, never in the image.
- **Start command of your own**: Set `RAILPACK_START_CMD` with `ops env set`.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves.

## v1.0.0-beta.11

November 4, 2026

**In short:** `${VAR}` in compose.yml takes your env vars from ops, so a Postgres service gets its password; update if your compose uses variables.

### Fixed
- **Variables in compose.yml**: `${DB_PASSWORD}` and the like now take the app's env vars from ops, as a `.env` next to the file would. Before, they came out empty. A service such as Postgres did not start.
- **Deploys while a database is down**: A Postgres that does not run no longer blocks the deploy that could fix it. The deploy log says there was no snapshot, and why.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves. Then deploy the app again.

## v1.0.0-beta.10

November 4, 2026

**In short:** Postgres gets what SQLite has: every change backed up, full copies, a daily restore check and the data browser; plus a new home page.

### New
- **Postgres backups**: Every change goes to your bucket within a minute, in seconds when it's busy. Go back to any second of the last 7 days: `ops restore --at "2026-11-04 14:02"`.
- **Postgres snapshots**: A full copy every hour and before every deploy, restore and edit. `ops backup`, `ops restore <snapshot>`.
- **Checked every day**: ops restores the newest Postgres copy into a throwaway database and checks it, like SQLite.
- **Postgres in the data browser**: Tables, rows, schema, search, edit and delete by primary key, and read-only SQL. Web: the **Data** tab. CLI: `ops db`, `ops sql`.
- **Rows by primary key**: SQLite tables without a rowid can be edited too.

### Changed
- **Home page**: "Deploy anything. Lose nothing. Open source." The product shots change by themselves.
- **Reads stay reads**: SQL that changes data is refused up front in a read, with a hint to use `--write`.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves. Deploy an app once so ops finds its Postgres.

## v1.0.0-beta.9

November 4, 2026

**In short:** A new domain on a server without a Cloudflare Tunnel gets its HTTPS certificate again; update if you add domains.

### Fixed
- **HTTPS for new domains**: Since beta.1, a domain added on a server that serves ports 80 and 443 itself got no Let's Encrypt certificate. It showed a browser warning. It gets a certificate again on the next deploy. Servers behind a Cloudflare Tunnel were never affected.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves. Then deploy each app with a new domain once.

## v1.0.0-beta.8

November 4, 2026

**In short:** The app page puts Deploy from GitHub first; deploying from CI waits behind one click.

### Changed
- **Deploy from CI**: Deploying an image and the deploy hook now sit folded under **Deploy from CI** on the app page. The GitHub **Deploy** is the one you see. An app with a hook shows it open.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves.

## v1.0.0-beta.7

November 4, 2026

**In short:** Apps and servers are slim cards you drag as a whole, the version of ops is always in sight, and changing repos on github.com no longer ends on an error.

### New
- **Version in the sidebar**: The hub's version sits at the bottom of the sidebar and opens the changelog.
- **Update pending**: A server that is behind says so next to its version. It updates itself on its next connect.

### Changed
- **Apps and servers lists**: Every app and server is a slim card of its own, with its grip on the left. Both pages look the same.
- **List and grid switch**: Icons only.

### Fixed
- **GitHub repos changed on github.com**: Coming back to ops no longer shows "GitHub is not connected". The change was saved all along.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves.

## v1.0.0-beta.6

November 4, 2026

**In short:** Every server now updates itself, so metrics arrive everywhere; charts move to a file of their own to keep the database small, and the app tabs fit on a phone.

### Changed
- **Charts in their own file**: Metrics and hourly uptime live in `history.db` next to the database, without a backup. The database stays small and a restore stays fast. The charts start over once. Outages and uptime state stay.
- **Home page**: The live numbers show from 1,000 deploys on.

### Fixed
- **Daemons that never updated**: A daemon built from a commit instead of a release, such as one installed from `main`, now updates itself like any older one.
- **Metrics page**: It no longer tells you to update servers by hand. They update themselves.
- **App tabs on a phone**: Overview, Data, Backups, Uptime and Metrics no longer push the page wider than the screen. Swipe them.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves.

## v1.0.0-beta.5

November 4, 2026

**In short:** Alerts go to Slack, Discord, Telegram and webhooks, every server and app has live charts, and each app can back up to its own bucket.

### New
- **Alerts where your team is**: Send alerts to Slack, Discord, Telegram or a signed webhook, as well as email. Each one has a test. Web: **Alerts**. CLI: `ops alerts add slack <name>`. Also the TUI settings, API and MCP. (#19)
- **Live metrics**: CPU, memory, disk and network of every server and app. Live for the last hour and back 30 days, with each deploy marked. Web: the **Metrics** tab. CLI: `ops metrics shop`. TUI: `m`. API and MCP too. (#20)
- **More than one bucket**: Give each app its own bucket for uploads and backups, say one per client. Web: **Buckets**. CLI: `ops buckets add`, `ops buckets use`. (#15)

### Changed
- **Buckets**: The bucket you had is now the default one. Older backups stay where they were. They restore from there when an app moves. (#15)
- **Request and error counts**: Per app, these are not in the metrics yet. Response times are on the Uptime tab.

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your servers update themselves. Until they do, metrics and per-app buckets wait.

## v1.0.0-beta.4

November 3, 2026

**In short:** ops watches your sites every minute and mails you when one goes down, puts apps and servers in your order, and updates itself and your servers.

### New
- **Uptime checks, built in**: ops checks every domain of every app from outside once a minute, from the first deploy on, with nothing to set up. The app's **Uptime** tab shows 24 hours, 7 and 30 days, the certificate and the last outages. Also `ops uptime`, the TUI (`u` on an app), API and MCP. (#18)
- **Your own order**: Drag apps and servers where you want them, or use `ops apps move blog top`, or `K` / `J` in the TUI. Everyone in the workspace sees the same order everywhere. (#14)
- **List or grid**: Switch how the Apps and Servers pages look. ops remembers it for you. (#14)
- **Update now**: A self-hosted ops shows when a release is out, with its notes. It updates in one click. If the new version does not start, the old one comes back. Also `ops update`. (#8)
- **Servers update themselves**: Each one moves to the hub's version, one server at a time and never during a deploy. Turn it off with `ops servers auto-update off`. (#8)
- **Email alerts**: Owners and admins get a mail when a site goes down or comes back, a server goes offline or a deploy fails. Turn it off for you with `ops alerts off`.

### Changed
- **Activity**: A server going offline and coming back now shows there. So does every site that goes down and comes back. (#18)
- **Self-hosted gets releases only**: `update.sh` takes the newest release, not every build in between.
- **A copy of the database before every upgrade**: ops writes `backup-before-<version>.db` next to it before it changes the schema. (#8)

### Fixed
- **Signing in with GitHub finds your account**: It uses any verified email on your GitHub account, not only the primary one. It no longer makes a new empty account. (#24)

### Upgrade
Self-hosted: run `git pull` in the repo, then `sh update.sh` once. It also starts the updater. From then on, use **Update now**.

## v1.0.0-beta.3

November 3, 2026

**In short:** Vim keys in the terminal, env vars as one `.env` file, and a dead server can finally go.

### New
- **Vim keys in the TUI**: `j` `k`, `gg` `G`, `/` to search, `:` for commands, `?` for help. Logs scroll, and `G` follows the end again. (#11)
- **Env vars as one `.env` text**: Paste or edit them all at once. Web: **Fields | Text**. CLI: `ops env push`. TUI: `e`, then `edit`. API `PUT /api/v1/apps/{app}/env`, MCP `replace_env`. ops shows what changes and asks first. (#9)

### Changed
- **TUI keys moved**: They make room for vim. GitHub `g` → `gh`, logs `l` → `L`, rename `n` → `cw`, deploy hook `h` / `H` → `:hook` / `:hook off`. (#11)
- **Admin is only on the hosted hub**: Self-hosted, you manage people under **Team**. `ADMIN_EMAILS` only says who may sign up.
- **Free forever is what you tick**: Hosted Admin only. Admin shows each user's plan: trial, trial ended, free forever or paying. It turns free forever on or off.

### Fixed
- **A dead server can be removed**: Offline, it goes with its apps. They stay on the machine if it comes back. Online, its apps still go first. (#21)

### Removed
- **The waitlist**: Sign-up is open. The first start deletes the `waitlist` table with its addresses. Whoever is in keeps their plan.

### Upgrade
Self-hosted: pull the new image and restart. Your servers need no update.

## v1.0.0-beta.2

November 2, 2026

**In short:** Sign in with GitHub in one click, and the first page asks only your name and your workspace.

### New
- **Sign in with GitHub first**: "Continue with GitHub" is the main way in. The email link stays below it. A password is optional in the settings. (#24)
- **One short first page**: Your name and your workspace name. From GitHub both are filled in, so it's one click. Invited people only give their name. (#24)
- **API**: `POST /api/v1/auth/onboarding` takes an optional `workspace_name`. (#24)

### Fixed
- **"Continue with GitHub" only shows when GitHub sign-in is set up**: It never leads to a GitHub error page.

### Upgrade
Self-hosted: pull the new image and restart. For GitHub sign-in set `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET`. A GitHub App's client ID and secret work, with "Email addresses: read-only" and `<your url>/auth/github/callback` as a redirect URI.

## v1.0.0-beta.1

November 2, 2026

**In short:** ops is out, as a paid beta: deploy your apps to your own servers, and keep every change of their SQLite database in your own bucket. Hosted is $29 a month or $249 a year per workspace, 30 days free, no card.

### New
- **Deploy**: From your repo, a GitHub branch or commit, or an image from CI. Every push to GitHub can deploy by itself. The commit shows the deploy status. A failed deploy brings the last good one back.
- **Backups**: Every SQLite change goes into your bucket. A snapshot before every change ops makes. Restore to any second. A restore check every day.
- **Data**: Browse tables, edit rows and run SQL on the live database.
- **Domains**: HTTPS, a canonical domain, and a password in front if you want one.
- **Security**: Cloudflare Tunnel instead of open ports. SSH only over Tailscale. A firewall, and a rules file on your server. The daemon dials out, and ops holds no key to your servers.
- **Teams and the activity log**: Roles, invitations, and who changed what, to the second.
- **Every surface**: Web, CLI, terminal UI, API and MCP.
- **Self-hosting**: Every feature, free, under AGPL.

### Upgrade
New install: see [getting started](https://ops.goilerplate.com/docs/getting-started). What comes next is on the [roadmap](https://ops.goilerplate.com/docs/roadmap).

