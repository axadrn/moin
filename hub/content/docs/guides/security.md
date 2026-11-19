---
title: "Security"
description: "Web, SSH and the firewall of a server. moin keeps them that way. Rules only root can change."
group: "Servers and data"
order: 2
---

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

SSH and the firewall always work. Only the Cloudflare Tunnel needs Cloudflare, and only SSH through Tailscale needs Tailscale. Until you connect them, the line has a short link to [Integrations](#integrations).

Every change asks first. The CLI too; `--yes` skips it in scripts. If you do one thing, turn on the firewall.

## moin keeps it

The daemon looks at its server after each start and every five minutes. What drifted, it puts back: the tunnel, the tailnet, SSH off, the firewall. Each fix is in the [activity](/docs/guides/activity), by moin: `put back the firewall on web-1`.

- It looks at what really is. A tailnet you joined by hand counts.
- It never opens a door by itself. If you stop SSH by hand, the line is red until you pick Off, or Everyone again.

## Integrations

The accounts a workspace connects once, for every app and server: GitHub, Cloudflare and Tailscale. Each says whether it is connected. A connected one has **New key**: save a new key there and it replaces the old one, without disconnecting. Disconnect sits at the end.

```surfaces
Web: **Settings**, **Integrations**
CLI: moin integrations
CLI: moin integrations cloudflare
CLI: moin integrations tailscale
CLI: moin integrations rm cloudflare
CLI: moin integrations rm tailscale
TUI: `w` opens the web
API: GET /api/v1/integrations
API: PUT /api/v1/integrations
MCP: get_integrations
MCP: set_integrations
```

`moin integrations` shows what is connected, never the keys. The CLI reads a key from stdin, so it never lands in your shell history. A new key replaces the old one, on every surface. The API and MCP disconnect when you send a key empty. Keys are stored encrypted.

- **Cloudflare**: an API token. In Cloudflare, **My Profile**, **API Tokens**, **Create Token**, **Create Custom Token**. Account **Cloudflare Tunnel: Edit** and Zone **DNS: Edit** for the zones of your apps. Zone **Zone Settings: Edit** is optional: with it moin turns on **Always Use HTTPS**, without it you do that in Cloudflare. moin checks the token before it stores it.
- **Tailscale**: an auth key. In the Tailscale admin console, **Settings**, **Keys**. Make it reusable.
- With Cloudflare connected, every new server gets the tunnel. Turn it off on its Security page.
- Turn the tunnels off before you disconnect Cloudflare. Without the token moin cannot delete a tunnel (`tunnels_on`). To change the token, save a new key instead: the tunnels keep running.
- Disconnecting Tailscale changes no server. Servers stay in your tailnet; new ones no longer join it.

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

- Only through Tailscale needs the server in a tailnet: joined by hand, or with Tailscale connected. Then the daemon installs Tailscale if it is missing and joins. Port 22 closes to everyone else only once Tailscale is up.
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
CLI: moin servers secure web-1
CLI: moin servers
TUI: on the server, **Access**
API: GET /api/v1/servers
MCP: list_servers
```

The CLI shows it in the ACCESS column, and `servers secure` line by line. The API has what is set in `tunnel`, `ssh` and `firewall`, and what is on the server in `security`. Without the firewall, every port that listens is open.

## Rules

A file on your server, `/etc/moin/rules`, says what moin may do there. Only you, as root on the server, can change it. The daemon checks every request against it.

Nobody can change it through moin: not us, not your team, not someone who breaks into moin.

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
| `data: off` | reading tables, running SQL and editing rows through moin |
| `logs: off` | reading container logs through moin; logs often hold what your users sent |
| `exec: off` | [running commands](/docs/guides/exec) in containers through moin |
| `deploy-from: <sources>` | deploys from anywhere else |
| `backups-to: <host>/<bucket>` | backups to and restores from any other bucket, also an app's own bucket or an older one |
| `network: locked` | changes to Web, SSH and the firewall: set them up first, then lock them. The daemon still puts back what drifts |
| `server-access: on` | nothing: it allows [containers that reach beyond the app](#containers-that-reach-into-the-server), which moin refuses without it |

- A `deploy-from` source is a GitHub repo `github.com/<owner>/<repo>`, an image without its tag like `ghcr.io/<owner>/<image>`, or `upload` for `moin deploy` from your computer (with `backups-to` set, an upload must come from that bucket too). `*` stands for one part: `github.com/<owner>/*` allows every repo of that owner. A redeploy or a rollback brings no new code, so it is always allowed.
- The `backups-to` host is your [bucket's](/docs/guides/bucket#add-it) endpoint without `https://`, like `<account-id>.r2.cloudflarestorage.com`; for AWS S3 `s3.<region>.amazonaws.com`.
- No file: moin may do everything, except [containers that reach into the server](#containers-that-reach-into-the-server). A rule left out allows what it covers. A file with a mistake, such as an unknown rule, refuses everything until you fix it.
- Rules apply at once. After a change, `systemctl restart moin-daemon` shows them everywhere.
- A server with rules never updates its daemon from moin ([Updates](/docs/guides/servers#updates)).
- A refused request says why: `refused by the rules of this server (/etc/moin/rules): data: off`.

### Containers that reach into the server

An app is a guest in your house. It gets its own room and stays there. Some containers ask for the master key instead: with it, they can open every door of the server. moin turns them away at the door, unless you say yes on the server.

So moin does not list what is forbidden. It lists what a normal app needs, and refuses everything else, with or without a rules file. A setting Docker adds tomorrow is refused too, until it is on the list.

The server checks the compose file as it will run, before anything builds or starts. That holds for every deploy: new code, a new image, new env and a rollback. A refused deploy says which setting: `service web: privileged is not allowed: this reaches beyond the app, so the server refuses it. Allow it with server-access: on in /etc/moin/rules`.

| Allowed | What for |
| --- | --- |
| `image`, `build` from the app's source | what runs |
| `command`, `entrypoint`, `environment`, `env_file` from the app's source, `working_dir`, `user` | how it runs |
| `ports`, `expose`, `networks` of the app, `links`, `hostname`, `extra_hosts`, `dns` | how it is reached |
| `volumes`: the app's data folders (`./data:/data`), files of its source, its named volumes, `tmpfs`, and `/etc/localtime` and `/etc/timezone` read-only | its data |
| `configs` and `secrets` written in the file or from env | its settings |
| `healthcheck`, `depends_on`, `restart`, `init`, `stop_signal`, `stop_grace_period` | how it starts, stops and is checked |
| limits: `mem_limit`, `cpus`, `pids_limit`, `ulimits`, `shm_size`, `deploy` resources and replicas | how much it may use |
| `cap_drop`, `read_only`, `security_opt: no-new-privileges`, `sysctls` starting with `net.` | fewer powers, never more |
| `labels`, `tty`, `stdin_open`, `platform`, `pull_policy`, `scale`, and `x-` extensions | the rest a normal app uses |

Everything else is refused. Some that come up:

| Refused | Why |
| --- | --- |
| `privileged`, `cap_add`, `devices`, `device_cgroup_rules`, GPUs | the kernel's powers or the server's hardware |
| `network_mode`, `pid`, `ipc`, `userns_mode`, `cgroup` | the server's network, processes or users, or another container's |
| `volumes_from` | the mounts of another container, like the proxy's Docker socket |
| a mount outside the app's data folders and source, like `/`, `/etc` or `/var/run/docker.sock`, or the app's own folder, which holds the compose files moin runs | the container reads or writes the server |
| a mount inside a folder a container can write | the container could swap it for a link to `/` while the deploy starts |
| named volumes with a `driver`, `driver_opts` or another name, networks of other apps | another disk, or another app's data |
| `configs` and `secrets` from a `file`, `env_file` outside the source | files of the server |
| `build` from outside the source, `additional_contexts`, `ssh`, `privileged` or `network` | the same, while the image builds |
| hooks like `post_start` | they can run with all powers |

- The list is one place in the code: `serviceKeys` and the lists after it in `internal/daemon/access.go`, every line with what it is for. The catalog uses none of the refused settings.
- Need one, like a monitoring tool that reads the Docker socket? Write `server-access: on` in `/etc/moin/rules`. Only you can, as root on the server, so someone who breaks into moin cannot. Then the list does not apply on that server at all, so put such a tool on a server of its own.
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
