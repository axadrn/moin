---
title: "Security"
description: "How moin keeps your servers safe, even if someone breaks into moin. Passkeys, sign-in, secrets, web, SSH, firewall, rules, the journal and signed updates."
group: "Servers and data"
order: 2
---

How moin keeps your servers safe. First in pictures, then the facts.

## Add a passkey. Everyone should.

A passkey is a key on your phone, laptop or password manager. You unlock it with your face, your finger or your screen lock. It takes a minute: **Settings**, **Account**, **Security**, **Add passkey**.

- **Nobody can phish it.** A passkey works only on your hub's own address. A fake page that looks like moin gets nothing, even when you fall for it.
- **It works without GitHub.** GitHub down, or your GitHub account lost or taken over? Your passkey still lets you in.
- **Nothing to leak.** The hub keeps only the public half. The secret half never leaves your device.

Until you have one, moin asks on every page. [Add a passkey](/docs/guides/account#add-a-passkey)

## How moin keeps you safe

moin has two parts. The **hub** is where you click and type: the web, the CLI, the TUI. The **daemon** runs on each of your servers and does the work. Think of the hub as a messenger. It brings your server letters, like "start this app".

**Nobody can ring your server's bell.** Your server calls the messenger, never the other way round.

- The daemon opens one outbound WebSocket to the hub, over HTTPS on port 443. moin listens on no port of your server.
- With the [firewall](#firewall) on, only 80 and 443 for your apps are open, and SSH as you set it.

**moin has no key to your house.** Other tools keep the root SSH key of every server, so whoever breaks into them is root on all your servers. moin keeps none.

- The daemon runs on the server and acts there. The hub holds no SSH key, password or token that opens your server.
- The hub never holds your code. Code comes only from a GitHub repo, an image or the catalog. The daemon downloads nothing else, so `deploy-from` decides every source.

**Your secrets stay in your house.** The messenger carries a secret to your server once, then forgets it. It keeps a list of names, never the values.

- Env values, bucket keys and registry logins live only on the server, in two files readable by root. [Where your secrets live](#where-your-secrets-live)

**You write the house rules.** A note on your door says what the messenger may do. Only you can write it, on the server. The messenger cannot change it.

- `/etc/moin/rules`, readable by root. The daemon checks every request against it. [Rules](#rules)
- The daemon writes every request in the server's journal, and whether it did it. [See what the hub asked](#see-what-the-hub-asked)

**New versions carry a seal.** A messenger cannot slip your server a fake daemon.

- Every release is signed with Ed25519. An update comes from GitHub, not from the hub, and the daemon checks the seal first. The first install trusts the hub you install from. [Updates](#updates)

**Every guest stays in its room.** An app gets its own folders and nothing else of the house. A container that asks for the master key is turned away at the door.

- The server checks every compose file against a list of what a normal app needs, before anything builds or starts. [Containers](#containers-that-reach-into-the-server)

**Two keys, no password.** You come in with GitHub or a passkey, not with a password moin keeps. There is no password to guess, to leak or to reset by email.

- Sign-in is GitHub, with two-factor as your GitHub account has it, or a passkey. [Sign in](#sign-in)

**What stays honest.** A messenger that someone bought can still bring letters your rules allow. It cannot take the house. With the default rules it can still empty the rooms: your apps, their secrets and their data. Strict rules leave it much less, not nothing. [If someone breaks into the hub](#if-someone-breaks-into-the-hub)

## Sign in

You sign in with GitHub or a passkey. Like a club that lets you in when a friend at the door knows you: GitHub knows you, moin asks GitHub. Or you bring your own key, the passkey, and moin checks it.

- **GitHub or a passkey, nothing else.** No passwords, no sign-in links by email. Nothing to guess, nothing to phish from an inbox, no reset that takes over an account. Sign-up is GitHub only; a passkey comes later, in the settings.
- **The GitHub account counts, not the email.** moin stores the id of your GitHub account. It never changes, even when you change your email or name on GitHub.
- **The first time only, the email.** An account made before GitHub finds its GitHub account by GitHub's verified primary email, if the account proved that address too and has no GitHub account yet. Secondary addresses and unverified ones never count. After that, only the id counts.
- **A taken address stays taken.** When your GitHub email belongs to another account, moin says so and signs you in nowhere.
- **Invitations.** The link in an invitation proves its mailbox. Any GitHub account may take it, and the new account gets the invited address.
- **New accounts.** Only while sign-up is open: on the hosted hub, or with an invitation. A self-hosted hub lets in its admins and invited people.
- **Every sign-in is a new session.** The cookie the browser had ends, so nobody can plant a session on you. Sessions live in an HttpOnly cookie only, never in a script.
- **Passkeys are WebAuthn.** A passkey belongs to the domain of the hub, so a look-alike site cannot use it. The hub stores the public key only. It asks for a passkey the device keeps itself (a discoverable credential) and for your face, finger or screen lock every time (user verification), and wants no attestation, so any passkey provider works.
- **Every passkey sign-in is checked in full.** The challenge is random, kept on the hub, works once and for five minutes. The signature, the origin, the domain and the flags are checked by go-webauthn, a maintained WebAuthn library, never by code of our own. A sign counter that goes back means a copied passkey: refused. 30 tries per 15 minutes per address.
- **A new passkey sends you an email**, so one you did not add does not go unseen. An operator impersonating you can neither add nor remove one.
- **The last way in stays.** Without GitHub, your last passkey cannot be removed. Add another first.
- **Recovery key.** Lost your GitHub account or your passkey? The operator of the hub runs `./main recovery-key <email>` on the server. The key is 160 random bits, works once, for one hour, and only its SHA-256 is stored. It is never logged and never in a link. Five tries per 15 minutes per address. It resets every way in: it signs the account out everywhere, ends its connections and CI tokens, disconnects its GitHub account and deletes its passkeys, so a stolen GitHub account or device no longer gets in. Then you connect your GitHub and add a passkey again.
- **An email-change link is no way in.** It changes the address and signs the account out everywhere. Only a browser already signed in to that account stays in.
- **Connecting GitHub and adding a passkey need a CSRF token**, so no other site can start them. [Recovery key](/docs/self-hosting#recovery-key)
- **The CLI, the TUI and AI agents** are connections: you let each one in from the browser with your passkey and pick its [rights](/docs/reference/rights). CI uses a CI token. Neither can ever change how you sign in or give itself more rights. [Account and connections](/docs/guides/account)
- **AI agents sign in with OAuth 2.1**, as the MCP spec asks: PKCE S256 only, redirect URIs exact and https or localhost, a code that works once for a minute, and tokens that work only at `/mcp`. Only on localhost, 127.0.0.1 and [::1] may the port differ, since an app there gets a free port each time. A redirect host must be plain ASCII, so no look-alike letters; the approval page shows it next to the agent's name, which is only the agent's own word. A wrong request goes back to the agent only when you click. [AI agents](/docs/guides/mcp)

## Ports, SSH and the firewall

Your server has doors to the internet, called ports. A server's **Security** page has three lines. Each says what really is on the server. The daemon keeps it that way.

| Line | What it is | Choices |
| --- | --- | --- |
| **Web** | How visitors reach your apps (ports 80 and 443, or a Cloudflare Tunnel). | Open, Cloudflare Tunnel |
| **SSH** | Who can log in to the server with SSH (port 22). | Everyone, Only through Tailscale, Off |
| **Firewall** | Blocks every port moin does not need (nftables, the firewall built into Linux). | On, Off |

```surfaces
Web: **Servers**, **Security** on the server
CLI: moin servers secure web-1 --tunnel on --ssh tailscale --firewall on
TUI: on the server, `W` opens its security page in the web
API: PUT /api/v1/servers/{id}/security
MCP: set_server_security
```

The dot in front of a line:

- **Green**: locked down, as set.
- **Quiet**: open, as set.
- **Red**: the server is not as you set it. A red sentence says both, like `SSH on the server is Off. moin says Everyone. Pick the one you want.`
- **Waiting for the server**: the daemon has not reported yet.

SSH and the firewall always work. Only the Cloudflare Tunnel needs Cloudflare, and only SSH through Tailscale needs a Tailscale key on the server. Until you have them, the line has a short link to [Integrations](#integrations).

Every change asks first. The CLI too; `--yes` skips it in scripts. If you do one thing, turn on the firewall.

## moin keeps it

The daemon looks at its server after each start and every five minutes. What drifted, it puts back: the tunnel, the tailnet, SSH off, the firewall. Each fix is in the [activity](/docs/guides/activity), by moin: `put back the firewall on web-1`.

- It looks at what really is. A tailnet you joined by hand counts.
- It never opens a door by itself. If you stop SSH by hand, the line is red until you pick Off, or Everyone again.

## Integrations

The accounts a workspace connects: GitHub, Cloudflare and Tailscale. Each says whether it is connected. A connected one has **New key**: save a new key there and it replaces the old one, without disconnecting. Disconnect sits at the end.

The Cloudflare token is for the whole workspace: the hub keeps it and uses it itself. The Tailscale key goes to one server, which keeps it. The hub never does. Tailscale lists the servers that have the key and the ones that do not. Enter it again for each one that needs it.

```surfaces
Web: **Settings**, **Integrations**
CLI: moin integrations
CLI: moin integrations cloudflare
CLI: moin integrations tailscale --server web-1
CLI: moin integrations rm cloudflare
CLI: moin integrations rm tailscale --server web-1
TUI: `w` opens the web
API: GET /api/v1/integrations
API: PUT /api/v1/integrations
MCP: get_integrations
MCP: set_integrations
```

`moin integrations` shows what is connected, never the keys. The CLI reads a key from stdin, so it never lands in your shell history. A new key replaces the old one, on every surface. The API and MCP disconnect when you send a key empty. With more than one server, name the server for the Tailscale key: `--server` in the CLI, `server` in the API and MCP. It must be online.

- **Cloudflare**: an API token. In Cloudflare, **My Profile**, **API Tokens**, **Create Token**, **Create Custom Token**. Account **Cloudflare Tunnel: Edit** and Zone **DNS: Edit** for the zones of your apps. Zone **Zone Settings: Edit** is optional: with it moin turns on **Always Use HTTPS**, without it you do that in Cloudflare. moin checks the token before it stores it.
- **Tailscale**: an auth key. In the Tailscale admin console, **Settings**, **Keys**. Make it reusable.
- With Cloudflare connected, every new server gets the tunnel. Turn it off on its Security page.
- Turn the tunnels off before you disconnect Cloudflare. Without the token moin cannot delete a tunnel (`tunnels_on`). To change the token, save a new key instead: the tunnels keep running.
- Removing the Tailscale key from a server changes nothing on it. It stays in your tailnet. The key only joins a server once.

## Web

How visitors reach your apps.

- **Open**: ports 80 and 443 are open. moin gets the HTTPS certificates itself.
- **Cloudflare Tunnel**: visitors come through Cloudflare. Ports 80 and 443 stay closed. Cloudflare serves HTTPS.

```bash
moin servers secure web-1 --tunnel on
```

With the tunnel, moin sends every domain of the server's apps through it and points their DNS at it. A domain you add later gets the same. It turns on **Always Use HTTPS**.

- If the token cannot reach a domain's zone, a red line says so: point the domain at `<tunnel>.cfargotunnel.com` yourself. Without Zone Settings Edit, turn on **Always Use HTTPS** yourself.
- Open again deletes the tunnel and the DNS records moin made. Point your domains back at the server with an A record ([Add a domain](/docs/guides/domains#add-a-domain)).
- A server behind the tunnel has no [instant address](/docs/guides/domains#instant-address): it needs ports 80 and 443.
- A new app starts with its [password](/docs/guides/domains#password) on, for every domain of it. Nobody reaches it before you, also not a setup page where the first visitor becomes the admin. Turn it off when the app is ready for the world.

## SSH

Who can log in to the server with SSH, on port 22.

| SSH | Means |
| --- | --- |
| **Everyone** | port 22 answers on every network |
| **Only through Tailscale** | port 22 answers only in your tailnet, your own private network |
| **Off** | the daemon stops sshd and keeps it stopped |

```bash
moin servers secure web-1 --ssh tailscale
```

- Only through Tailscale needs the server in a tailnet: joined by hand, or with a Tailscale key on that server. Then the daemon installs Tailscale if it is missing and joins. Port 22 closes to everyone else only once Tailscale is up.
- Off is for a server you reach with Tailscale SSH or your provider's console. Going back from off starts sshd again.
- Before you close SSH, keep a second way in, such as your provider's web console.

## Firewall

The firewall that is built into Linux: nftables. Ubuntu, Debian and Fedora have it already.

- **On**: moin adds its own table, named `moin`. It lets in what the other lines need, answers to connections the server made itself, and ping. It drops everything else. The line says what is open to everyone, like `80, 443`.
- **Off**: moin removes its table. Rules of your own stay. With SSH only through Tailscale, the table keeps one rule: port 22 only in the tailnet.
- Without nftables, the line shows a red problem. Install it, and moin turns the firewall on by itself within five minutes:

```bash
apt install nftables   # Ubuntu, Debian
dnf install nftables   # Fedora
```

This is the firewall on the server. Your provider may have a cloud firewall in front of it. moin does not touch that one.

## See what is open

```surfaces
Web: **Servers**, **Security** on the server
CLI: moin servers
TUI: on the server, **Access**
API: GET /api/v1/servers
MCP: list_servers
```

The CLI shows it in the ACCESS column. `moin servers secure web-1` prints it line by line too, but it needs the servers right: it applies the settings again. The API has what is set in `tunnel`, `ssh` and `firewall`, and what is on the server in `security`. Without the firewall, every port that listens is open.

## Where your secrets live

On your server, and only there. The hub passes a value on once, the moment you enter it, and forgets it. It keeps the name and when it changed.

| File on the server | What is in it |
| --- | --- |
| `/var/lib/moin/apps/<app>/app.env` | the [env vars](/docs/guides/env) of one app |
| `/var/lib/moin/apps/<app>/releases/<n>/htpasswd` | the hash of the app's [password](/docs/guides/domains#password), for deploy n |
| `/var/lib/moin/server.json` | the secrets of the server: the secret keys of your [buckets](/docs/guides/bucket), the [registry logins](/docs/guides/ci#registry-logins), the Tailscale key and the token of the Cloudflare Tunnel |

All are readable by root only. No other file of moin holds a value: not the compose file of a deploy, not its logs, not what the server tells the hub. A test proves it on every change. It deploys an app that uses a secret every way a compose file can, then searches every file of the daemon and every message it sends. Deploys from before moin 1.0.0-beta.35 wrote values into their compose file: that holds once an app deployed from its source again, and its old deploys are cleaned up after 5 builds. [In compose.yml](/docs/guides/env#in-composeyml) A secret goes to a server only while it is online. When it is offline, moin says so and keeps nothing to send later.

**What the hub keeps.** Names and dates, never a value:

- the names of your env vars, and when each changed
- each bucket's endpoint, bucket, folder and access key ID
- each registry's host and username
- which server has which key
- the app's password as a hash only. moin shows it once, when it makes it
- the command line of a [command in a container](/docs/guides/exec), with the app's env values masked. Do not type a secret into it

**What stays with the hub.** The hub uses these itself, so it has to keep them, encrypted with its own key:

- The Cloudflare API token. With it, someone who breaks into the hub can change the DNS of your zones and your tunnels. Give the token only the zones your apps use.
- Alert targets: Slack and Discord webhooks, Telegram bot tokens, webhook URLs, and the webhook signing secret. With them, someone can post in your channels as moin, or sign fake alerts. Use a channel or bot made only for moin.
- On a self-hosted hub, the GitHub App it created: its private key, client secret and webhook secret. With them, someone can read the repos you gave the app and set their commit statuses. Give the app only the repos you deploy.

**What stays honest.**

- A value you type in after someone broke into the hub passes through it once. They could read that one.
- The secrets of a [catalog app](/docs/guides/catalog) never pass the hub: the server makes them. Only the first sign-in password, which moin shows you once, passes it once.
- The hub can still send what your [rules](#rules) allow. With the default rules it reads every secret of an app three ways: a command in a container prints the env (`exec: off` closes it), new code deployed over the app gets the env (`deploy-from` narrows it), and SQL that writes runs commands in a Postgres container (`data: off` closes it). [If someone breaks into the hub](#if-someone-breaks-into-the-hub)
- A variable used in `image`, `ports`, `expose`, `volumes` or `user` is filled in before the hub sees the compose file, so its value is not secret. Keep secrets in `environment`.
- A server rebuilt from scratch starts without secrets. Enter them again, then deploy. A later server gets the bucket key, registry login and Tailscale key once more: the pages show which server lacks one.
- The proxy, Traefik, sees the apps folder read-only, to read the password's hash. So it can read every app's `app.env` too. We accept that: it runs with the Docker socket, which is as much as root on the server anyway. A bug in Traefik that reads files would expose them, so moin keeps Traefik current.
- An app moin builds with [Railpack](/docs/concepts/how-moin-works) has a hash of its env in its compose file, so a changed value builds again. A hash, not a value.
- The env names moin shows come from the server: each daemon reports which keys its apps have, never the values. A key the server does not have is not shown.

**How Docker gets the env.** Every Docker Compose command moin runs for an app gets its `app.env`: as `env_file` of the app's services, and as `--env-file` to fill `${NAME}` in the compose file the moment the containers start. The compose file itself keeps `${NAME}`. Docker never sees the values as its own settings, so an env var like `DOCKER_CONFIG` or `DOCKER_HOST` cannot steer it.

## Rules

A file on your server, `/etc/moin/rules`, says what moin may do there. Only you, as root on the server, can change it. The daemon checks every request against it.

Nobody can change it through moin: not us, not your team, not someone who breaks into moin.

A new server gets the file when you install the daemon. Every rule is in it, with the value moin takes without the file, so you see what moin may do there. A second install keeps your file.

```
# What moin may do on this server. Only root can change this file, the hub cannot.
# The daemon checks every request against it, at once. A mistake refuses everything until it is fixed.
# These are the values moin takes without the file. Docs: Security, Rules.

# off: no tables, SQL, row edits or backup downloads through moin.
data: on
# off: no container logs through moin.
logs: on
# off: no commands in containers through moin.
exec: on
# locked: no changes to web, SSH and the firewall through moin.
network: open
# on: containers may reach beyond the app, like privileged ones or the Docker socket.
server-access: off
# Deploys only from these sources. Without it: from anywhere.
# deploy-from: github.com/<owner>/*, ghcr.io/<owner>/*, catalog
# Backups and restores only with this bucket. Without it: any bucket.
# backups-to: <endpoint-host>/<bucket-name>
# Folders of the server that apps may mount. Without it: none.
# mounts: /mnt/media
```

Lock it down by changing a value, or by writing a rule without its `#`:

```
# /etc/moin/rules
data: off
logs: off
exec: off
deploy-from: github.com/<owner>/*, ghcr.io/<owner>/*
backups-to: <endpoint-host>/<bucket-name>
network: locked
```

| Rule | What it stops |
| --- | --- |
| `data: off` | reading tables, running SQL, editing rows and downloading backups through moin. With Postgres, SQL that writes can run commands in the database container, so this one matters next to `exec: off`. The copies still go to any bucket: set `backups-to` too |
| `logs: off` | reading container logs through moin; logs often hold what your users sent |
| `exec: off` | [running commands](/docs/guides/exec) in containers through moin |
| `deploy-from: <sources>` | deploys from anywhere else |
| `backups-to: <host>/<bucket>` | backups to and restores from any other bucket, also an app's own bucket or an older one |
| `network: locked` | changes to Web, SSH and the firewall: set them up first, then lock them. The daemon still puts back what drifts |
| `mounts: <folders>` | nothing: it lets apps mount these folders of the server, like `/mnt/media`, which moin refuses without it |
| `server-access: on` | nothing: it allows [containers that reach beyond the app](#containers-that-reach-into-the-server), which moin refuses without it |

- A `deploy-from` source is a GitHub repo `github.com/<owner>/<repo>`, an image without its tag like `ghcr.io/<owner>/<image>`, or `catalog` for the apps of the [catalog](/docs/guides/catalog), whose compose files come with the signed daemon. `*` stands for one part: `github.com/<owner>/*` allows every repo of that owner. An image deploy of an app that follows a repo brings both, the image and the commit for its `compose.yml`, so `deploy-from` must list both. The hub deploys a commit named by its SHA only when it is on the app's branch, as a fork's commit is not. That check is the hub's. Against a hacked hub, `deploy-from` holds for private repos. For a public repo, a fork's commit, which GitHub also serves through the repo, passes. `deploy-from` is per server, not per app: an allowed source can be deployed over any app of the server. A redeploy or a rollback brings no new code, so it is always allowed.
- The `backups-to` host is your [bucket's](/docs/guides/bucket#add-it) endpoint without `https://`, like `<account-id>.r2.cloudflarestorage.com`; for AWS S3 `s3.<region>.amazonaws.com`.
- No file, as on a server installed before the install wrote one: moin may do everything, except [containers that reach into the server](#containers-that-reach-into-the-server). A rule left out allows what it covers. A file with a mistake, such as an unknown rule, refuses everything until you fix it.
- Rules apply at once. After a change, `systemctl restart moin-daemon` shows them everywhere.
- A server with rules updates its daemon like any other ([Updates](#updates)). Each release carries our signature, so a new daemon cannot drop the rules.
- A refused request says why: `refused by the rules of this server (/etc/moin/rules): data: off`.

### Containers that reach into the server

An app is a guest in your house. It gets its own room and stays there. Some containers ask for the master key instead: with it, they can open every door of the server. moin turns them away at the door, unless you say yes on the server.

So moin does not list what is forbidden. It lists what a normal app needs, and refuses everything else, with or without a rules file. A setting Docker adds tomorrow is refused too, until it is on the list.

The server checks the compose file as it will run, before anything builds or starts. That holds for every deploy: new code, a new image, new env and a rollback. A refused deploy says which setting: `service web: privileged is not allowed: this reaches beyond the app, so the server refuses it. Allow it with server-access: on in /etc/moin/rules`.

moin fills in `${NAME}` in the image, the ports, the volumes and the `build` paths when it reads the file, so it checks them as they will run. In another setting it checks, such as an `env_file` path or the name of a network, `${NAME}` is refused: it could become anything when the containers start.

| Allowed | What for |
| --- | --- |
| `image`, `build` from the app's source | what runs |
| `command`, `entrypoint`, `environment`, `env_file` from the app's source, `working_dir`, `user` | how it runs |
| `ports`, `expose`, `networks` of the app, `links`, `hostname`, `extra_hosts`, `dns` | how it is reached |
| `volumes`: the app's data folders (`./data:/data`), files of its source read-only (`./nginx.conf:/etc/nginx/nginx.conf:ro`), its named volumes, `tmpfs`, `/etc/localtime` and `/etc/timezone` read-only, and the folders of `mounts:` | its data |
| `configs` and `secrets` written in the file or from env | its settings |
| `healthcheck`, `depends_on`, `restart`, `init`, `stop_signal`, `stop_grace_period` | how it starts, stops and is checked |
| limits: `mem_limit`, `cpus`, `pids_limit`, `ulimits`, `shm_size`, `deploy` resources and replicas | how much it may use |
| `cap_drop`, `read_only`, `security_opt: no-new-privileges`, `sysctls` starting with `net.` | fewer powers, never more |
| `labels`, `tty`, `stdin_open`, `platform`, `pull_policy`, `scale`, and `x-` extensions | the rest a normal app uses |
| `extends` of a service in the same file | services that share settings |

Everything else is refused. Some that come up:

| Refused | Why |
| --- | --- |
| `privileged`, `cap_add`, `devices`, `device_cgroup_rules`, GPUs | the kernel's powers or the server's hardware |
| `network_mode`, `pid`, `ipc`, `userns_mode`, `cgroup` | the server's network, processes or users, or another container's |
| `volumes_from` | the mounts of another container, like the proxy's Docker socket |
| a mount of a folder of the server, like `/`, `/etc`, `/srv/data` or `/var/run/docker.sock`, unless `mounts:` lists it, or of the app's own folder, which holds the compose files moin runs | the container reads or writes the server |
| the app's source writable, like `.:/app` | a container could leave a link to `/` there for the next build or mount to follow. Mount it read-only (`.:/app:ro`) and keep data in `./data` |
| a data folder inside one a container can write, like `./data` and `./data/db` together | the container could swap `db` for a link to `/` while the deploy starts. Side by side is fine: `./data/db` and `./data/uploads` |
| named volumes with a `driver`, `driver_opts` or another name, networks of other apps | another disk, or another app's data |
| `configs` and `secrets` from a `file`, an `env_file` outside the source | files of the server, read into the app |
| `include`, `extends` of another file, `label_file` | they read other files, which could be anywhere on the server. Put the services and labels in one compose file |
| a network with its own `ipam` subnet | the subnet could cover the server's own network and take its traffic; Docker picks a free one |
| `build` from outside the source, `additional_contexts`, `ssh`, `privileged`, `network` or `tags` | the same, while the image builds. A tag like `traefik:v3.6` would replace moin's own proxy |
| hooks like `post_start` | they can run with all powers |

- The list is one place in the code: `serviceKeys` and the lists after it in `internal/daemon/access.go`, every line with what it is for. The catalog uses none of the refused settings.
- Your data stays where it is: `./data` is a folder of the app on the server, `/var/lib/moin/apps/<app>/volumes/data`, so you see it with `ls`.
- A folder of the server, like a disk with your films for Jellyfin? Write `mounts: /mnt/media` in `/etc/moin/rules`. Apps may then mount exactly these folders, and nothing next to them.
- Need more, like a monitoring tool that reads the Docker socket? Write `server-access: on` in `/etc/moin/rules`. Only you can, as root on the server, so someone who breaks into moin cannot. Then the list does not apply on that server at all, so put such a tool on a server of its own.
- A deploy that does not come up stops its containers, so none of them keeps running half-started.
- Backups, restores and the data browser touch an app's SQLite files only inside the folder they were found in, through Go's `os.Root`. A link the app leaves there never leads the daemon, which runs as root, out of it. Copies and checks are made in the daemon's own folder, which no app can reach.
- What it does not stop: a deploy of a normal app still runs that app's code, with its data and env. Against code from somewhere else, set [`deploy-from`](#rules).

### See the rules of a server

```surfaces
Web: **Servers**, **Security** on the server, **Rules**
CLI: moin servers rules web-1
TUI: on the server, **Access**, under the rules
API: GET /api/v1/servers
MCP: list_servers
```

`moin servers` shows them in its RULES column; the API in the `rules` field.

## If someone breaks into the hub

A messenger someone bought cannot take your house: it never becomes root on the server, never changes the rules, never hides from the journal and never installs a daemon we did not sign. But it can still bring every letter your rules allow. With the default rules, that is everything your apps hold.

| | With the default rules a hacked hub can | With strict rules it still can |
| --- | --- | --- |
| Commands | run any command in any app's container: it sees the env and the data | nothing, with `exec: off` |
| Deploys | deploy any image or repo over any app. The new code starts with that app's env and data | deploy a source `deploy-from` allows over any app of the server, like a catalog app over your shop. For a public repo, also a fork's commit |
| Databases | read and write every database. With Postgres, SQL that writes runs commands in the database container (`COPY … TO PROGRAM`) and reads its files | nothing, with `data: off` |
| Backups | send every copy to a bucket of its own, or restore one of its own. Also with `data: off` | nothing, with `backups-to` |
| Env vars | set and remove them, like a proxy or a database host of its own | the same: no rule covers env vars |
| Password and domains | turn the app's password off, put a domain on a service you never published | the same: no rule covers them |
| Logs | read every container's logs | nothing, with `logs: off` |
| Web, SSH, firewall | open them | nothing, with `network: locked` |
| Stop, remove, restore | stop and remove apps, restore an older copy. A remove keeps the volumes | the same |

- The journal shows each request, with the env keys it set: [See what the hub asked](#see-what-the-hub-asked).
- The same holds for a stolen [connection or CI token](/docs/reference/rights), within its rights. The deploy right runs new code in an app, and new code sees the app's env and data. The servers right can invite a person, and a member deploys, sets env and runs SQL.

## See what the hub asked

Your server keeps a diary. Every letter the messenger brings goes in it: what it asked, and whether the daemon did it or said no, and why. The messenger cannot tear out a page: the diary is on your server.

```
journalctl -u moin-daemon
```

```
Nov 25 09:14:02 server-1 moin[812]: 2026/11/25 09:14:02 INFO hub asked type=deploy.start app=my-app deploy=d-41 number=12 source=api.github.com/repos/acme/blog/tarball/abc123
Nov 25 09:14:31 server-1 moin[812]: 2026/11/25 09:14:31 INFO done deploy=d-41
Nov 25 09:20:45 server-1 moin[812]: 2026/11/25 09:20:45 INFO hub asked type=exec.start request=r-7 app=my-app service=web command=psql
Nov 25 09:20:45 server-1 moin[812]: 2026/11/25 09:20:45 WARN not done request=r-7 why="refused by the rules of this server (/etc/moin/rules): exec: off"
```

- One line for what was asked, one for what came of it. `request` or `deploy` ties the two together.
- It names the app and what the request is about: the deploy number, the source, the image, the command, the env keys, a secret's name, the bucket, SSH and the firewall. Never an env value, a secret, a key, a password, SQL, the arguments of a command or a file.
- Why a request was not done shows in full when the rules refused it. Any other error says `why=failed`, as it may hold what a command printed: the hub shows the whole error.
- `journalctl -u moin-daemon -f` follows it as it happens. `--since today` shows one day.

## Updates

A daemon only updates to a release we signed. Think of a stamp: we stamp every release, and every daemon carries a photo of the stamp. It compares the two before it installs anything. The photo cannot stamp, so it can be public.

1. The hub tells the daemon: update to `v1.2.3`. It names the version and nothing else.
2. The daemon downloads `SHA256SUMS` and `SHA256SUMS.sig` of that release from GitHub, `github.com/axadrn/moin/releases`.
3. It checks the signature against the public keys built into it. A wrong signature stops the update.
4. It downloads its binary from the same release and checks it against the signed SHA-256.
5. It runs the new binary once, which has to say `moin v1.2.3`, and only then swaps itself.

- It never goes back to an older release, and never installs a build that is not a release.
- The signing key never touches the hub. Someone who takes over the hub cannot sign, so they cannot hand your server a daemon.
- In GitHub the key sits in a protected environment. Only a release tag reaches it, and only after a maintainer approves. A workflow or an action someone slipped in cannot sign.
- A daemon without systemd cannot restart itself, so it does not update itself. Update it by hand: take the binary from the release and check it first (below).
- The first install trusts the hub you install from. The install script and the binary come from it, and nothing checks them. To trust only our signature, take the binary from the release and check it (below).
- A hub update pulls the image by its version tag. Nothing checks its signature. To be sure, check `SHA256SUMS` and pull the image by its digest (below).
- There are two keys: one signs every release, a spare stays offline. If the first ever leaks, a release signed with the spare drops it, and every daemon stops trusting it.
- The keys are Ed25519, in `releaseKeys` in `internal/daemon/update.go`. Forging a signature without the private key is not possible with today's computers. What matters is that nobody steals the key.
- Your servers need to reach `github.com` and `release-assets.githubusercontent.com`, where GitHub sends the downloads, to update. Your apps do not.
- Check a download by hand: the signature of the checksums with our public key, then the binary against the checksums.

```
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/moin-linux-amd64
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/SHA256SUMS
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/SHA256SUMS.sig
cat > moin-release.pem <<'KEY'
-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAdkwXdu3Ecl8Rs4tfXxIrvipOfRa5R61tfWEdKISw3LA=
-----END PUBLIC KEY-----
KEY
openssl pkeyutl -verify -pubin -inkey moin-release.pem -rawin -in SHA256SUMS -sigfile SHA256SUMS.sig
sha256sum --check --ignore-missing SHA256SUMS
```

- `Signature Verified Successfully`, then `OK` next to the binary. Anything else: do not run it. The key is the first of `releaseKeys`.

- The same signed `SHA256SUMS` names the hub image of the release by its digest, in the line `<digest>  ghcr.io/axadrn/moin:v1.2.3`. Pull exactly that image, by its digest:

```
docker pull "ghcr.io/axadrn/moin@sha256:$(awk '$2 == "ghcr.io/axadrn/moin:v1.2.3" { print $1 }' SHA256SUMS)"
```
