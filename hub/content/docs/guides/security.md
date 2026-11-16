---
title: "Security"
description: "Who reaches your apps, who reaches SSH, and the firewall. moin keeps it that way. Rules only root can change."
group: "Servers and data"
order: 2
---

Your server has doors to the internet, called ports. A server's **Security** page says, in three lines, which doors are open. Each line shows what really is on the server, a dot and one action. The daemon keeps it that way.

```text
outside                    server
moin ◀──────────────────── daemon
visitors ─▶ Cloudflare ◀── cloudflared
you, SSH ─▶ tailnet ◀───── Tailscale
                           firewall:
                           the rest
                           is shut
```

Each line starts on your server and calls out. With the tunnel, SSH only through Tailscale and the firewall, only 41641/udp stays open to everyone. It is Tailscale's port for direct connections.

| Line | What it says | Needs |
| --- | --- | --- |
| **Visitors** | Cloudflare Tunnel, or ports 80 and 443 open | Cloudflare connected, for the tunnel |
| **SSH** | everyone, only through Tailscale, or off | Tailscale, for only through Tailscale |
| **Firewall** | on or off: on drops everything the other lines do not need | nothing: turn it on for every server |

```surfaces
Web: **Servers**, **Security** on the server
CLI: moin servers secure web-1 --tunnel on --ssh tailscale --firewall on
TUI: on the server, `W` opens its security page in the web
API: PUT /api/v1/servers/{id}/security
MCP: set_server_security
```

- A green dot: locked down, as set. A quiet one: open, as set. A red one: the server differs from what is set, and the line says both, like `Everyone · set: Only through Tailscale`. Before the daemon reports, it says `Waiting for the server`.
- SSH and the firewall always work. The tunnel needs Cloudflare, and SSH only through Tailscale needs Tailscale. Until then, the line shows a short link, **Connect Cloudflare** or **Connect Tailscale**. It opens [Integrations](#integrations).
- Every change asks first. The CLI too; `--yes` skips it in scripts. If you do one thing, turn on the firewall.

## moin keeps it

The daemon checks its server after each start and every five minutes. What drifted, it puts back: the tunnel, the tailnet, sshd off, the firewall. Each fix is in the [activity](/docs/guides/activity), by moin: `put back the firewall on web-1`.

- It looks at what really is. A tailnet you joined by hand counts. An sshd that listens while SSH is off is stopped again.
- It never opens a door by itself. If you stop sshd by hand, SSH says `Off · set: Everyone` until you pick one.

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

## Visitors

With the tunnel, visitors reach your app through Cloudflare, so the server keeps ports 80 and 443 closed.

moin creates a tunnel, sends every domain of the server's apps through it, and points each domain's DNS at it. It turns on **Always Use HTTPS**. Cloudflare serves HTTPS, so the server needs no certificate. A domain you add later gets the same.

```bash
moin servers secure web-1 --tunnel on
```

- If the token cannot reach a domain's zone, a red line under the three says so: point the domain at `<tunnel>.cfargotunnel.com` yourself. Without Zone Settings Edit it says to turn on **Always Use HTTPS** yourself.
- Off deletes the tunnel and the DNS records moin made. Point your domains back at the server with an A record ([Add a domain](/docs/guides/domains#add-a-domain)).
- A server behind the tunnel has no [instant address](/docs/guides/domains#instant-address): it needs ports 80 and 443.

## SSH

SSH is the door you use to log in to your server yourself.

| SSH | Means |
| --- | --- |
| **Everyone** | SSH answers on every network |
| **Only through Tailscale** | SSH answers only in your tailnet, your own private network |
| **Off** | the daemon stops sshd and keeps it stopped |

```bash
moin servers secure web-1 --ssh tailscale
```

- Only through Tailscale needs the server in a tailnet: joined by hand, or with Tailscale connected. Then the daemon installs Tailscale if it is missing and joins. Port 22 closes to everyone else only once Tailscale is up.
- Off is for a server you reach with Tailscale SSH or your provider's console. Going back from off starts sshd again.
- Before you close SSH, keep a second way in, such as your provider's web console.

## Firewall

The firewall shuts every door the other lines do not need.

```bash
moin servers secure web-1 --firewall on
```

It lets in answers to connections the server made itself, ping, and what the other lines leave open. It drops everything else. With the firewall on, the line lists what is open to everyone.

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
| `network: locked` | changes to the tunnel, SSH and the firewall: set them up first, then lock them. The daemon still puts back what drifts |

- A `deploy-from` source is a GitHub repo `github.com/<owner>/<repo>`, an image without its tag like `ghcr.io/<owner>/<image>`, or `upload` for `moin deploy` from your computer (with `backups-to` set, an upload must come from that bucket too). `*` stands for one part: `github.com/<owner>/*` allows every repo of that owner. A redeploy or a rollback brings no new code, so it is always allowed.
- The `backups-to` host is your [bucket's](/docs/guides/bucket#add-it) endpoint without `https://`, like `<account-id>.r2.cloudflarestorage.com`; for AWS S3 `s3.<region>.amazonaws.com`.
- No file: moin may do everything. A rule left out allows what it covers. A file with a mistake, such as an unknown rule, refuses everything until you fix it.
- Rules apply at once. After a change, `systemctl restart moin-daemon` shows them everywhere.
- A server with rules never updates its daemon from moin ([Updates](/docs/guides/servers#updates)).
- A refused request says why: `refused by the rules of this server (/etc/moin/rules): data: off`.

### See the rules of a server

```surfaces
Web: **Servers**, **Security** on the server, **Rules**
CLI: moin servers rules web-1
TUI: on the server, **Access**, under the rules
API: GET /api/v1/servers
MCP: list_servers
```

`moin servers` shows them in its RULES column; the API in the `rules` field.
