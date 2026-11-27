---
title: Changelog
description: What shipped in moin, newest first.
---

## v1.0.0-beta.38

November 27, 2026

**In short:** moin without a hub. One command deploys over your own SSH, public SSH closes after it, and every backup is encrypted and checked every day.

### Why
The hub was the one place every server trusted. Whoever took it over could reach every server behind it. Now commands go one way: from your laptop to your server, over SSH, with your key. Reports go one way: from your server to a watcher. Nothing in between holds a key. The story: [Why I deleted the hub](/blog/why-i-deleted-the-hub).

### New
- **Deploy over SSH**: `moin deploy prod` sends the committed code of this folder to your server, and the server builds it. If `ssh prod` works, it works. (#171)
- **Set up by the first deploy**: The first `moin deploy prod` installs Docker and moin on the server, after you say yes.
- **Live without DNS**: The first deploy ends in an HTTPS address, like `https://shop.203-0-113-7.sslip.io`.
- **One command for everything**: `moin` shows every server you deployed to, its apps and what needs you. On a terminal it is a small live view: logs, backups, restart and rollback.
- **Public SSH closes**: After the first deploy, moin asks to close port 22 and use Tailscale. It closes it only once a new SSH session over Tailscale works.
- **`moin secure`**: The web through a Cloudflare Tunnel, and a firewall for every other port. No public TCP port is left.
- **`moin unlock`**: Locked out? Type it in your provider's web console. Port 22 opens again.
- **Knocks**: `moin knocks` shows what the firewall dropped, by port.
- **Encrypted backups**: Every copy is encrypted on the server before it leaves, for the server's key and for yours.
- **Your backup key**: `moin bucket set prod` shows it once. `moin bucket confirm prod` proves it by opening a copy on your laptop.
- **Restore when the server is gone**: On a new server, `moin restore shop --from prod --to <copy> --key` brings the copy back with your key.
- **`moin follow`**: The server deploys each new commit of a branch, read with a deploy key it makes. CI needs no key to your server.
- **The team is SSH keys**: `moin keys add prod anna.pub` lets Anna in. `moin keys rm prod anna@laptop` takes her out.
- **Alerts from the server**: A failed backup, a failed check, a crashing app or a full disk, to Telegram, Slack, Discord or a webhook. `moin alerts add prod telegram --token … --chat-id …`.
- **Your own watcher**: `moin watcher run`, on another machine. It alerts when a server goes silent, or a domain is down from outside.
- **moin cloud**: A watcher we run for you, with alerts by mail. `moin cloud login`, then `moin cloud connect prod`. 30 days free from the first report, then $7 a month or $49 a year. (#174)
- **A copy nobody can delete**: moin cloud keeps a second copy of every backup. Your server can add one, never delete one. TODO(check): ships in beta.38, or only on the landing page?

### Changed
- **Nobody else can command your server**: Only who has SSH to it as root. Not a watcher, not moin cloud, not us.
- **Code comes from your laptop**: A deploy sends your committed code over SSH, not from GitHub. `--image shop:dev` sends a local image instead.
- **Your server never updates by itself**: `moin update prod` puts your CLI's version on it, signed and checked. The CLI says once a day when a new release is out.
- **Settings live on the server**: The bucket, the alerts, the watchers and the backup schedule are in `/etc/moin/config.yml`, for root only.
- **A failed copy stops the deploy**: moin copies the databases right before a deploy. If that fails, the deploy stops. `moin deploy --no-copy` goes on without it. TODO(check): new in beta.38?
- **Apache-2.0**: moin is under Apache-2.0 now, not AGPL. [What it covers](/docs/faq#is-moin-open-source)

### Removed
- **The hosted hub**: moin.run no longer runs your servers. moin cloud watches them, and holds no key.
- **Self-hosting a hub**: There is nothing to host. Run your own watcher with `moin watcher run`.
- **The web app**: The CLI is the interface. `moin` shows everything.
- **The API and the MCP server**: Scripts and AI agents use the CLI, with `--json`.
- **Accounts, passkeys, teams and roles**: Whoever has SSH to the server is the team. `moin keys add prod anna.pub`.
- **`moin login`, CI tokens and the GitHub App**: Deploy from your laptop, or let the server follow a branch with `moin follow`.
- **The old terminal UI**: `moin` opens the new one.
- **The data grid, the SQL editor and metrics**: `moin exec shop -- psql`.
- **The app password**: `moin password` is gone. Use the app's own sign-in, or Cloudflare Access in front of it.
- **The rules file**: `/etc/moin/rules` is gone. No hub can command a server, so there is nothing to narrow.
- **Daemon updates by themselves**: Run `moin update prod`.
- **moin UI and moin next**: `/ui` and `/next` are gone.

### Upgrade
Read all steps first. Your apps and their data stay. What lived in the hub does not: domains, the bucket, alerts.

1. Install the new CLI on every laptop: `curl -fsSL https://moin.run/cli.sh | sh`.
2. On each server from before, once: `ssh -t prod moin daemon uninstall`. The apps keep running, with their data.
3. A tunnel from before: `ssh prod docker rm -f moin-cloudflared`. A firewall from before: `ssh prod nft delete table inet moin`. After step 5, `moin secure prod` sets them up again.
4. The app password is gone. An app behind it is public after step 5. Give it a sign-in of its own, or put Cloudflare Access in front of it.
5. In each app's folder: `moin deploy prod`. It takes the app over, with its data and env. The folder needs the app's name, or add `--app <name>`. A catalog app: `moin deploy prod --catalog umami`. The web pauses for a few seconds.
6. Right after it, add your domains again: `moin domains add shop shop.example.com`. Until then they do not answer. The instant address does.
7. Set the bucket and the alerts again: `moin bucket set prod`, then `moin alerts add prod …`. Your old copies stay in the bucket, but moin no longer lists them. They are plain `.db.gz` and `.dump` files in a folder named by a long ID. Delete it once the new copies cover you.
8. Hosted accounts: `moin cloud login` with the same mail, then `moin cloud connect prod`. Early backers get their discount at the checkout.
9. CI that deployed through the hub: remove that step, then `moin follow shop --branch main`.
10. Self-hosted hub: in `/opt/moin`, `docker compose down --rmi all`, then `rm -rf /opt/moin /var/lib/moin-hub /etc/moin-hub /etc/moin/rules`.

## v1.0.0-beta.37

November 26, 2026

**In short:** beta.36 did not start on a hub with people signed in. This fixes it, and an update now tries the new hub on a copy of your database before it replaces the running one.

### New
- **Every update tries first**: `update.sh` starts the new hub once on a copy of your database, with your settings. If that fails, it says why and stops. The running hub stays. (#159)
- **Install your way**: One server stays the default. `--hub-only` runs the hub on its own server, with HTTPS from Caddy. Add `--tunnel-token` for a Cloudflare Tunnel and no open port, or `--no-proxy` for your own proxy. (#161)

### Changed
- **One settings file**: A self-hosted hub keeps its settings in `/opt/moin/.env`, next to its compose files. The domain is written once, as `DOMAIN`. One line, `COMPOSE_FILE`, says which way it runs. (#161)
- **The tunnel gets only its token**: `cloudflared` no longer sees the hub's other settings. (#161)

### Fixed
- **beta.36 starts on every hub**: A change to the database stopped the hub when someone was signed in. It runs now, and keeps everyone signed in. (#158)

### Removed
- **Update now**: A self-hosted hub no longer updates itself from the web, `moin update` or the MCP tool `update_hub`. It shows what is new and the command. Run `sh /opt/moin/update.sh` on the server. The hub never gets Docker, not even to update itself. (#160)
- **Settings from 1Password in `update.sh`**: `env.tpl` and `op-token` are no longer filled. Write your settings into `/opt/moin/.env`. (#161)

### Upgrade
Self-hosted, once: move your settings to `/opt/moin/.env` and fetch the new files. The steps: [Coming from before v1.0.0-beta.37](/docs/self-hosting#coming-from-before-v100-beta37). Your daemons update by themselves.

## v1.0.0-beta.36

November 26, 2026

**In short:** Name your passkeys, confirm it's you without signing out, and catalog apps get back a secret that beta.35 left behind.

### New
- **Name your passkeys**: Name one right when you add it, or rename it later under **Settings**, **Account**, **Security**. moin suggests the name of your password manager. (#154)
- **Confirm it's you**: Adding a passkey, more rights or a new connection asks you to confirm, and you stay signed in. With a passkey you confirm with it. Without one, a sign-in or GitHub from the last five minutes counts. (#156)

### Changed
- **Passkeys carry the hub's domain**: Your password manager files them under `moin.run`, or your own hub's domain. (#154)
- **A hub update tries the new hub first**: It starts the new hub once with your settings. If one is missing or wrong, it says why, and the running hub stays. (#151)

### Fixed
- **A missing registry login shows under Needs you**: When the app's server has no login for the registry of its image, the overview says so before the next deploy stops. (#153)
- **Catalog apps keep every secret**: beta.35 could leave out a secret of a catalog app, like Umami's `APP_SECRET`. The daemon now takes it back from the running app. (#150)
- **Backups checked today**: The overview counts only backups that run. A stopped backup is not checked. (#152)
- **Compose file of an older deploy**: moin says to deploy again, instead of pointing at an empty log. (#152)

### Upgrade
Self-hosted: **Update now**, or `sh update.sh`. Your daemons update by themselves. Coming from beta.34, read the Upgrade steps of beta.35 first.

## v1.0.0-beta.35

November 26, 2026

**In short:** Your secrets live only on your server, every login gets the rights you pick, and hosted moin costs $7 a month.

### Why
Someone who breaks into a hub should find nothing worth stealing. This release moves every secret value to your server, gives every login its own rights, and writes every request of the hub into your server's journal. Some changes break old setups, so read the Upgrade steps first.

### New
- **Passkeys**: Sign in with one. Add it under **Settings**, **Account**, **Security**. (#111)
- **Connections**: `moin login`, the TUI and AI agents sign in in the browser. You pick what each may do: read, deploy, secrets and data, delete, servers. More rights need your passkey. See and remove them under **Settings**, **Connections**. (#135)
- **AI agents sign in in the browser**: Add `https://moin.run/mcp` in Claude, Claude Code, Cursor or ChatGPT. The browser opens, you pick the rights. No token to copy. (#140)
- **CI tokens**: For scripts and CI. Each has its own rights, for one app or all. (#135)
- **See the compose file**: Every app shows the compose file moin runs, with the proxy and without your secret values. Save it, put a `.env` next to it, run it anywhere. Web, `moin compose`, API and MCP. (#131)
- **One password per app**: One switch guards the app's domains and its instant address. New apps start with it on. (#129)
- **Uptime per domain**: Each domain is watched on its own. Down after 3 failed visits, up after 3 answers. (#115)
- **Your server's journal**: Every request of the hub, and what came of it, in `journalctl -u moin-daemon`. Never a secret value. (#134)
- **The rules, written down**: A new server gets `/etc/moin/rules` with every rule and its value. (#137)
- **GitHub App in one click**: A self-hosted hub creates its GitHub App with one button. (#138)
- **A blog**: Guides on servers, backups and deploys at `/blog`. (#128)
- **moin on X**: News at [@usemoin](https://x.com/usemoin). (#146)

### Changed
- **$7 a month or $49 a year**: Hosted moin, everything unlimited. The 30 free days start when your first server connects, not at sign-up. (#132)
- **Sign in with GitHub or a passkey**: Passwords and sign-in links are gone. Everyone signs in once more. (#110)
- **Secrets live on your server**: Env values, catalog passwords, bucket keys and registry logins stay on your server. The hub sets them and never reads them. Setting one needs the server online. (#112, #113, #123, #130)
- **compose.yml is plain Docker**: Domains, the password and backups are set in moin only. A compose file with an `x-moin` block stops the deploy and says why. moin finds the port in `ports`, `expose`, `PORT`, then the Dockerfile's `EXPOSE`. (#124)
- **The instant address follows the password**: An app with a domain and no password of its own no longer guards its instant address. Turn on the password to guard it. (#129)
- **A domain is unique per app**: Two apps may use the same domain. (#125)
- **Code comes from GitHub, an image or the catalog**: `moin deploy` deploys the pushed commit. Your server fetches code only from GitHub, over HTTPS. (#106, #107)
- **A rollback keeps today's settings**: It runs the old code with today's env, domains and password. (#130)
- **Only what a normal app needs**: `privileged`, the Docker socket or a folder of the server are refused unless `/etc/moin/rules` allows them. (#96)
- **Personal tokens are CI tokens**: Read stays read. Read and write becomes read, deploy and secrets, without delete and servers. (#135)
- **Reading data needs "secrets and data"**: Database rows, SQL and backup downloads. Changing rows also needs delete. `exec` needs deploy, secrets and delete. (#135)
- **121 API requests a minute**: Per connection or CI token. More get "try again in N seconds". (#139)
- **Deploy every push is on by default**: In the API and MCP too, unless the app has a deploy hook.
- **Servers with rules update too**: Like any other server. (#102)
- **A CLI that is too old says so**: The hub answers it with the command that updates it. (#148)

### Fixed
- **1 update**: The TUI says "1 update", not "1 updates". (#93)

### Removed
- **Passwords, sign-in links, password reset**: Use GitHub or a passkey. (#110)
- **`x-moin`**: Set domains, the password and backups in moin. Use Docker's `expose` and `healthcheck`. (#124)
- **Folder uploads**: `moin deploy --dir` is gone. Push to GitHub, or deploy an image. (#106)
- **Reading env values**: `moin env --show`, `moin env pull`, `moin run` and MCP `get_env` are gone. Values are write-only. (#112)
- **The instant password**: The app has one password now. (#129)
- **`moin protect`**: Now `moin password <app>` and `moin password off <app>`. API `/apps/{app}/password`, MCP `set_password` and `remove_password`. `POST /apps` answers `{app, password}`. (#144)
- **MCP aliases**: `name` for app and server is gone. `remove_bucket`, `set_default_bucket` and `set_bucket_key` take `name`.
- **`GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `API_URL`**: A self-hosted hub signs in through its one GitHub App. (#110)

### Upgrade

Read all steps first.

1. Self-hosted, before you update: a GitHub App you made by hand needs the account permission **Email addresses: Read** and the callback URL `https://<your hub>/auth/github/callback`.
2. Update. Self-hosted: **Update now**, or `sh update.sh`, then `chmod 700 /var/lib/moin-hub`. Your daemons update by themselves and keep the env values of every app. Check each app once with `moin env <app>`.
3. A server with `/etc/moin/rules` updates once by hand, as root: `curl -fsSL https://<your hub>/download/moin-linux-amd64 -o /usr/local/bin/moin.new && chmod 755 /usr/local/bin/moin.new && mv /usr/local/bin/moin.new /usr/local/bin/moin && systemctl restart moin-daemon` (`arm64` on ARM).
4. Update the CLI on every computer: `curl -fsSL https://moin.run/cli.sh | sh`. Then `moin login`.
5. Enter bucket keys and registry logins once more: **Backups**, or `moin buckets key <name> --server <server>`. Backups pause until you do.
6. Remove the `x-moin` block from each compose.yml and move its port to `expose`. moin already took over its domains.
7. Give CI tokens the rights they now miss under **Settings**, **Connections**: delete to remove apps or run `moin exec`, servers to manage servers.
8. An app guarded only by the instant password asks for a new one after its next restart: `moin password off <app>`, then `moin password <app>`.
9. Self-hosted: once all works, delete the hub's database copies from before this update (`backup-before-*.db` in `/var/lib/moin-hub`, and older hub backups). They still hold the old secrets.

## v1.0.0-beta.34

November 19, 2026

**In short:** A safer moin. Daemons install only signed releases, and an app's env vars reach only the app.

### New
- **The way to a live app**: Your first visit shows the steps from a server to an app that answers. (#91)

### Changed
- **Signed daemon updates**: Your daemons take each update straight from the GitHub release and check its signature first. Someone who takes over a hub cannot hand them anything else. Your servers need to reach `github.com` to update. (#94)

### Fixed
- **An app's env cannot steer Docker**: Env vars reach your app and nothing else. Before, one like `DOCKER_CONFIG` could make Docker run a program from the app's code. (#98)

### Upgrade

Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`. Your daemons update by themselves.

## v1.0.0-beta.33

November 16, 2026

**In short:** Changing SSH or the firewall saves right away, and a mismatch says what is wrong.

### Changed
- **Clear words when the server differs**: A red line says it, for example "SSH on the server is Off. moin says Everyone. Pick the one you want."
- **Each line says what it is about**: SSH names port 22. Web names ports 80 and 443. The firewall names nftables.

### Fixed
- **Saving security is fast again**: Changing SSH or the firewall no longer waits on Cloudflare. It took up to 11 seconds and sometimes failed with red DNS errors.

### Upgrade

Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`. Your daemons update by themselves.

## v1.0.0-beta.32

November 16, 2026

**In short:** Security that tells the truth and keeps itself. Your keys in one place.

### New
- **Integrations in Settings**: GitHub, Cloudflare and Tailscale on one page. A new key replaces the old one, without disconnecting. (#81)
- **Security that tells the truth**: Each line shows what really is on the server. When it differs from what you set, it says both. (#82)
- **moin keeps it**: The daemon looks every five minutes and puts back what drifted. Each fix is in the activity. (#82)
- **SSH off**: A third choice next to Everyone and Only through Tailscale. Port 22 closes.
- **Hub backup in Settings**: Admins see whether the hub's own database is backed up, and when the last copy landed. (#84)

### Changed
- **Web, SSH and Firewall in plain words**: Each line says in one sentence what it is. Visitors is now Web.
- **SSH and the firewall always work**: Without Cloudflare or Tailscale too. Only the tunnel and SSH through Tailscale need them.
- **Hosted moin is for businesses**: Subscribe asks you to confirm you subscribe as a business. (#83)
- **Settings, GitHub is now Settings, Integrations**: Same place, with Cloudflare and Tailscale next to it.
- **API: `ssh` replaces `ssh_tailscale`**: It takes `everyone`, `tailscale` or `off`.
- **CLI: `--ssh everyone` replaces `--ssh open`**: And `--ssh off` is new.

### Fixed
- **The firewall says when nftables is missing**: The line names the package to install. moin turns the firewall on by itself once it is there.

### Upgrade

Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`. Your daemons update by themselves. Scripts send `ssh` instead of `ssh_tailscale`, and `--ssh everyone` instead of `--ssh open`.

## v1.0.0-beta.31

November 15, 2026

**In short:** Pages load faster and never show old styles after an update.

### Changed
- **Faster pages**: The web loads one stylesheet and one script instead of about fifty files.

### Fixed
- **No old styles after an update**: After a release, some pages showed old styles for hours, like a gray square instead of an icon. Every file now carries the version in its address, so browsers and CDNs always load the new one.

### Upgrade

Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`. Your daemons update by themselves.

## v1.0.0-beta.30

November 15, 2026

**In short:** Clearer apps. See where an app deploys from, find registries and IDs, and see only the databases that exist.

### New
- **Deploy from**: One card on the Deploys tab: Folder, GitHub or CI. A green dot shows the one in use. `moin apps <app>` shows it too, the API and MCP call it `deploy_from`.
- **Registries in the workspace**: Settings, Registries, right under Bucket.
- **IDs to copy**: The workspace, each app and each server show their ID at the end of their settings.

### Changed
- **One place at a time**: Linking a repo turns the deploy hook off. Turning the hook on unlinks the repo. moin asks first, `--yes` skips it.
- **Domains from compose.yml everywhere**: The app overview, the API, the CLI and MCP list them.
- **`moin hook` links the docs**: A full link to the CI workflow, on your hub.

### Fixed
- **Deploy hooks after a restart**: A restart no longer stops the hook from deploying new tags.
- **Deleted databases leave the list**: At the next deploy or restart. Their copies stay in your bucket.

### Upgrade

Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`. Your daemons update by themselves.

## v1.0.0-beta.29

November 15, 2026

**In short:** Safer deploy hooks. A leaked hook URL can only ship new versions of your own image.

### Changed
- **Deploy hooks deploy only new tags**: A hook deploys new tags of the image running now. Another image gets `422`. Any registry works, Docker Hub too. **Deploy from CI** and `moin hook` show the image. (#70)
- **First image by hand**: An app that runs from its repo needs one image deploy by hand. Then the hook takes over. To change the image, deploy the new one by hand. The hook follows, its URL stays. (#70)
- **A calmer Deploy from CI**: One line says what runs. A new hook shows in one box, with a link to add it to your CI. (#70)

### Upgrade

Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`. Your daemons update by themselves.

## v1.0.0-beta.28

November 15, 2026

**In short:** Updates you can trust. The daemon does its own steps. Your hub shows the notes first.

### New
- **Daemon updates do their own steps**: Some updates need something first, like stopping the apps that use SQLite. moin does it. You never do a step by hand. If a step fails, moin puts everything back and alerts you with `server.daemon_update_failed`. (#58)
- **Read the notes before you update**: On a self-hosted hub, **Update now** shows the whole release notes first. So do `moin update` and MCP. (#58)
- **How moin ships**: One page says how a change reaches your server. Pull requests, deploys, releases, images and daemon updates. See `/docs/reference/shipping`.
- **What the daemon does**: How moin works now says who talks to whom and what the daemon may do on your server.
- **The catalog in the search**: An empty search shows the catalog. Settings and the search link to the source code.

### Changed
- **`latest` is the newest release**: Like `stable`. A pull without a tag never gets work in between.
- **`update_hub` asks first**: The MCP tool shows the notes. It updates only with `confirm`.
- **Settings say Version**: The row that opens the changelog is called **Version**.

### Upgrade

Self-hosted: **Update now**, or `sh update.sh`. CLI: `curl -fsSL https://moin.run/cli.sh | sh`. Your daemons update by themselves.

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
- **GitHub sign-in says what went wrong**: A failed sign-in shows one clear line. The email field stays clean.
- **Select lists work with the keyboard in Safari**: The arrow keys move. The list stays open.

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

