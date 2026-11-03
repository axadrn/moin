---
title: "Security"
description: "No open port, SSH only on your tailnet, everything else dropped"
order: 8
---

A [server](/docs/guides/servers) ops runs can have no open port at all. ops sets up three switches per server, and each one closes a door:

- **Cloudflare Tunnel.** Visitors reach your apps through Cloudflare instead of ports 80 and 443. A tunnel is a connection your server opens to Cloudflare, so nothing has to come in.
- **SSH only on Tailscale.** SSH, the way you log in to the server, answers only on your tailnet. [Tailscale](https://tailscale.com) is a free service that links your own devices in a private network, your tailnet.
- **Firewall.** The server drops every connection the other switches do not need.

The agent only dials out to ops, so ops itself needs no open port either.

You can turn on each switch alone. If you only do one thing, turn on the firewall. The tunnel needs your domains in a Cloudflare account; SSH on Tailscale needs a Tailscale account.

## Keys

The switches need two keys: a Cloudflare API token for the tunnel and a Tailscale auth key for SSH. They belong to the workspace, work for all its servers and are stored encrypted. On the web they are under **Integrations** on the security page of any server.

On the server, the agent hands the Cloudflare token to cloudflared through its environment and the Tailscale key through a file only root can read, deleted right after. Neither shows up in `ps`.

### Store a key

Adds the Cloudflare token or the Tailscale key. A new key replaces the old one; an empty field keeps it.

```surfaces
Web: **Servers**, **Security** on the server, **Integrations**
CLI: ops integrations cloudflare
CLI: ops integrations tailscale
TUI: settings tab, **Cloudflare token** or **Tailscale key**, then `enter`
API: PUT /api/v1/integrations
MCP: set_integrations
```

The CLI reads the key from stdin, so it never ends up in your shell history.

### See which keys are stored

Shows whether a Cloudflare token and a Tailscale key are there, never the keys themselves.

```surfaces
Web: **Servers**, **Security** on the server, **Integrations**
CLI: ops integrations
TUI: settings tab
API: GET /api/v1/integrations
MCP: get_integrations
```

### Remove a key

Deletes a key from ops. Turn the tunnels off first: without the token ops cannot delete a tunnel in Cloudflare.

```surfaces
Web: **Servers**, **Security** on the server, **Integrations**, then **Remove Cloudflare token** or **Remove Tailscale key**
CLI: ops integrations rm cloudflare
CLI: ops integrations rm tailscale
TUI: settings tab, pick the key, `x`, then `y`
API: PUT /api/v1/integrations
MCP: set_integrations
```

The API and MCP remove a key when you send it empty.

Removing a key changes no server right away. Without the Tailscale key, the next security change of a server opens SSH again on every network. Turning the tunnel or SSH on Tailscale back on needs a key again.

### Create the keys

- **Cloudflare token:** in Cloudflare under **My Profile**, **API Tokens**, **Create Token**, **Create Custom Token**. Give it Account **Cloudflare Tunnel: Edit** and Zone **DNS: Edit**, for the zones of your apps. Zone **Zone Settings: Edit** is optional: with it ops turns on **Always Use HTTPS** for your domains, without it you turn that on in Cloudflare yourself. A zone is a domain you added to Cloudflare, such as `example.com`. ops checks the token before it stores it.
- **Tailscale key:** in the Tailscale admin console under **Settings**, **Keys**. Make it reusable.

## Cloudflare Tunnel

Sends all web traffic of a server through Cloudflare and closes ports 80 and 443. Use it when you want no web port open on the server.

```surfaces
Web: **Servers**, **Security** on the server, turn on **Cloudflare Tunnel**, then **Apply**
CLI: ops servers secure <server-name> --tunnel on
TUI: on the server, `t`, then `y`
API: PUT /api/v1/servers/{id}/security
MCP: set_server_security
```

With the tunnel on, `cloudflared` runs next to Traefik, the proxy of the server, and keeps a connection open to Cloudflare. Visitors reach Cloudflare, Cloudflare sends their requests through the tunnel and Traefik hands them to your app.

ops does the setup with your Cloudflare token:

1. It creates a tunnel for the server and starts `cloudflared`.
2. It routes every domain of the server's apps through the tunnel. Anything else gets a 404.
3. It points the DNS of each domain at the tunnel, in the Cloudflare zone that holds the domain.
4. It turns on **Always Use HTTPS** for those zones. Cloudflare serves HTTPS, so the server needs no certificate.

A domain you add later gets the same setup. If the token cannot reach the zone of a domain, **What the agent set up** says so: point the domain at `<tunnel>.cfargotunnel.com` yourself. Without Zone Settings Edit it says so too: turn on **Always Use HTTPS** in Cloudflare yourself.

Turning the tunnel off deletes it and the DNS records ops pointed at it. Traefik opens ports 80 and 443 again and gets certificates from Let's Encrypt. Point your domains back at the server with an A record, as in [Add a domain](/docs/guides/domains#add-a-domain).

## SSH only on Tailscale

Lets SSH answer only on your tailnet, so nobody else on the internet can even try to log in. Use it when you reach your servers from your own devices only.

```surfaces
Web: **Servers**, **Security** on the server, turn on **SSH only on Tailscale**, then **Apply**
CLI: ops servers secure <server-name> --ssh tailscale
TUI: on the server, `s`, then `y`
API: PUT /api/v1/servers/{id}/security
MCP: set_server_security
```

The agent installs Tailscale when it is missing and adds the server to your tailnet. Port 22 closes only once Tailscale is up and has an address. Until then it stays open and ops says why. It closes with the firewall off too: the agent then drops only SSH from other networks.

Before you close SSH, keep a second way into the server, such as the web console of your provider.

## Firewall

Drops every connection the server does not need. Turn it on for every server: without it, any program that listens on a port is open to the internet.

```surfaces
Web: **Servers**, **Security** on the server, turn on **Firewall**, then **Apply**
CLI: ops servers secure <server-name> --firewall on
TUI: on the server, `f`, then `y`
API: PUT /api/v1/servers/{id}/security
MCP: set_server_security
```

The firewall lets in answers to connections the server made itself, ping, and what the other switches leave open: SSH unless it is on Tailscale, ports 80 and 443 unless the tunnel carries the traffic, and port 41641 for Tailscale. It drops everything else.

The agent keeps its rules in one nftables table, `ops`, and changes them in one step, so there is never a moment without rules. nftables is the firewall built into Linux. Docker publishes container ports on its own, past this table. With the tunnel on, no container port is published.

## Apply on the web

On the web, flip the switches and press **Apply**. The switches are in the **Access** box. Apply changes only the switches you flipped: a change someone made meanwhile in the CLI, the TUI or through an agent stays. The page updates by itself when the server changes, except while a flipped switch waits for Apply.

## See what is open

Shows what the agent set up on a server: how traffic comes in, which ports are open to everyone, and the tailnet address.

```surfaces
Web: **Servers**, **Security** on the server, **What the agent set up**
CLI: ops servers
TUI: the ACCESS column on the servers tab
API: GET /api/v1/servers
MCP: list_servers
```

In the CLI it is the ACCESS column, and `ops servers secure` prints it after each change. In the API it is the `security` field. Without the firewall, **Open to everyone** lists only the ports ops uses, but every other listening port is open too.

## No open port

With all three switches on, nothing is open to the internet. The agent dials out to ops, `cloudflared` dials out to Cloudflare and Tailscale dials out to its own servers. **What the agent set up** then shows **Open to everyone: 41641/udp**, the Tailscale port, and nothing else.

## Rules

Rules are a file on your server that says what ops may do there. The agent checks every request against it and refuses what it does not allow. Nobody can change the file through ops: not us, not your team, not someone who breaks into ops. Changing it takes root on the server.

Write one rule per line into `/etc/ops/rules`:

```
# /etc/ops/rules
data: off
logs: off
deploy-from: github.com/<owner>/*, ghcr.io/<owner>/*
backups-to: <endpoint-host>/<bucket-name>
network: locked
```

| Rule | What it stops |
| --- | --- |
| `data: off` | reading tables, running SQL and editing rows through ops |
| `logs: off` | reading container logs through ops; logs often hold what your users sent |
| `deploy-from: <sources>` | deploys from anywhere else, see below |
| `backups-to: <host>/<bucket>` | backups to and restores from any other bucket, also an app's own bucket or an older one |
| `network: locked` | changes to the tunnel, SSH on Tailscale and the firewall. Set them up first, then lock them |

A source in `deploy-from` is one of these:

- a GitHub repo, as `github.com/<owner>/<repo>`,
- an image without its tag, such as `ghcr.io/<owner>/<image>`,
- `upload`, for `ops deploy` from your computer. With `backups-to` set, an upload must come from that bucket too.

`*` stands for one part, so `github.com/<owner>/*` allows every repo of that owner. A redeploy or a rollback brings no new code, so it is always allowed.

For `backups-to`, the endpoint host is the endpoint of [your bucket](/docs/guides/bucket#add-it) without `https://`, such as `<account-id>.r2.cloudflarestorage.com`. For AWS S3 it is `s3.<region>.amazonaws.com`.

Without the file, ops may do everything. A rule you leave out allows what it covers. A file with a mistake, such as an unknown rule, refuses everything until you fix it, so a typo never lets through what it was meant to stop.

Rules apply to every request right away. The agent reports them when it connects, so restart it after a change to show the new rules everywhere:

```bash
systemctl restart ops-agent
```

### See the rules of a server

Shows the rules a server has, as its agent reported them.

```surfaces
Web: **Servers**, **Security** on the server, **Rules**
CLI: ops servers rules <server-name>
TUI: the RULES column on the servers tab
API: GET /api/v1/servers
MCP: list_servers
```

`ops servers` shows them in its RULES column too. In the API they are the `rules` field.

A refused request says why, on every surface: `refused by the rules of this server (/etc/ops/rules): data: off`.

### Why it matters

`data: off` and `logs: off` keep your users' data from passing through ops. `deploy-from` is the other half: without it, whoever controls ops could deploy code that copies your database. With both, a break-in at ops reads nothing and runs nothing of its own on your server.
