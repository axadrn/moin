---
title: "Security"
description: "No open port, SSH only on your tailnet, everything else dropped"
order: 8
---

A server ops runs can have no open port at all. The agent only dials out; app traffic can come through a Cloudflare Tunnel, SSH can answer only on your Tailscale network, and a firewall drops the rest. Each is a switch per server, and ops sets it all up.

| Action | Web | CLI | TUI | MCP |
| --- | --- | --- | --- | --- |
| Store the keys | server, **Security**, **Keys** | `ops integrations cloudflare` · `tailscale`, key on stdin | – | `set_integrations` |
| Remove a key | **Keys**, **Remove Cloudflare token** · **Remove Tailscale key** | `ops integrations rm cloudflare` · `tailscale` | – | `set_integrations` with an empty key |
| Cloudflare Tunnel | **Cloudflare Tunnel**, then **Apply** | `ops servers secure web-1 --tunnel on` | `t`, then `y` | `set_server_security` |
| SSH only on Tailscale | **SSH only on Tailscale**, then **Apply** | `ops servers secure web-1 --ssh tailscale` | – | `set_server_security` |
| Firewall | **Firewall**, then **Apply** | `ops servers secure web-1 --firewall on` | `f`, then `y` | `set_server_security` |
| See what is open | **What the agent set up** | `ops servers` (ACCESS) and the output of `secure` | ACCESS column | `list_servers` |
| See the rules of a server | **Rules** | `ops servers rules web-1`, `ops servers` (RULES) | RULES column | `list_servers` |

On the web, flip the switches and press **Apply**. Apply changes only the switches you flipped: a change made meanwhile in the CLI, the TUI or by an agent stays. The page updates by itself when the server changes, except while a flipped switch waits for Apply.

**What the agent set up** shows the traffic, what is open to everyone and the tailnet. Without the firewall, **Open to everyone** lists the ports ops uses (SSH, and 80 and 443 without the tunnel); every other port a program listens on is open too. **Tailnet** shows the server's tailnet address, or says to add a Tailscale auth key when there is none.

## Keys

The keys are for all servers of the workspace and stored encrypted. Typing a new one replaces the old; an empty field keeps it. To take a key away, press **Remove Cloudflare token** or **Remove Tailscale key**, or run `ops integrations rm cloudflare` or `tailscale`. Removing a key changes no server at once. Turn the tunnels off first: without the token ops cannot delete a tunnel in Cloudflare. Without the Tailscale key, the next change of a server's security opens SSH again on every interface. Turning the tunnel or SSH on Tailscale on needs a key again.

## Cloudflare Tunnel

With the tunnel on, `cloudflared` runs next to Traefik and holds an outbound connection to Cloudflare. Visitors reach Cloudflare, Cloudflare sends their requests through the tunnel, Traefik routes them to your app. Ports 80 and 443 close: Traefik publishes no port at all.

ops does the setup with your own Cloudflare API token:

1. It creates a tunnel for the server and starts `cloudflared` with its token.
2. It routes every domain of the server's apps through the tunnel, and sends anything else to a 404.
3. It points each domain's DNS at the tunnel as a proxied CNAME, in the zone of your account that holds it.
4. It turns on **Always Use HTTPS** for those zones. Cloudflare serves HTTPS; there is no certificate on the server.

When you add a domain to an app, ops routes and points it the same way. A domain in a zone the token cannot reach shows up under **What the agent set up**: point it at `<tunnel>.cfargotunnel.com` yourself.

Create the token under **My Profile, API Tokens** with these permissions: Account **Cloudflare Tunnel: Edit**, Zone **DNS: Edit** and Zone **Zone Settings: Edit**, for the zones of your apps. ops checks it before storing it, encrypted.

Turning the tunnel off deletes it; Traefik takes ports 80 and 443 again and gets certificates from Let's Encrypt. Point your domains back at the server.

## SSH only on Tailscale

With a Tailscale auth key stored, the agent installs Tailscale when it is missing and joins the server to your tailnet. SSH then answers only on the tailnet interface. Port 22 closes only once Tailscale is up and has an address; until then it stays open and ops says why.

Create a reusable auth key in the Tailscale admin console under **Settings, Keys**. Keep one way into the server you trust, such as your provider's web console, before you close SSH.

## Firewall

The agent owns one nftables table, `ops`. Inbound it accepts answers to connections the server made, loopback, ping, and what the other switches leave open: SSH unless it is on Tailscale, ports 80 and 443 unless the tunnel carries the traffic, and Tailscale's port 41641. Everything else is dropped. The table changes in one transaction, so there is never a moment without rules.

Containers are not filtered by this table: Docker publishes their ports on its own chains. With the tunnel on, nothing is published.

## No open port

The tunnel, SSH on Tailscale and the firewall together leave nothing open to the internet. The agent dials out to ops, `cloudflared` dials out to Cloudflare and Tailscale dials out to its coordination server. **What the agent set up** shows **Open to everyone: 41641/udp**, the Tailscale port, and nothing else.

## Rules

Your server decides what ops may do there, not ops. Write the rules into `/etc/ops/rules` on the server, one per line. The agent reads them for every request and refuses what they do not allow. Nobody can change them through ops: not us, not your team, not someone who breaks into ops. Changing them takes root on the server.

```
# /etc/ops/rules
data: off
logs: off
deploy-from: github.com/acme/*, ghcr.io/acme/*
backups-to: s3.eu-central-1.amazonaws.com/acme-backups
network: locked
```

| Rule | What it stops |
| --- | --- |
| `data: off` | tables, SQL and row edits through ops, from the web, the CLI, the terminal UI and MCP |
| `logs: off` | container logs through ops; they often hold what your users sent |
| `deploy-from: <sources>` | deploys of anything else. A source is a GitHub repo as `github.com/<owner>/<repo>`, an image without its tag such as `ghcr.io/acme/web`, or `upload` for `ops deploy` from your computer. `*` matches one part, so `github.com/acme/*` allows every repo of acme. Redeploys and rollbacks bring no new code and stay allowed |
| `backups-to: <host>/<bucket>` | backups to or restores from any other bucket. With `upload` in `deploy-from`, an upload must come from this bucket too |
| `network: locked` | changes to the Cloudflare Tunnel, SSH on Tailscale and the firewall. Set them up first, then lock them |

Without the file, ops may do everything. Leave out a rule and ops may do what it covers. A file with a mistake, such as an unknown rule, refuses everything until you fix it, so a typo never lets through what it meant to stop. The agent reports the rules when it connects: restart it after a change, with `systemctl restart ops-agent`, to show the new ones everywhere. They apply to every request at once either way.

A refused request says so, in the web, the CLI, the terminal UI and MCP: `refused by the rules of this server (/etc/ops/rules): data: off`.

### Why it matters

`data: off` and `logs: off` keep your users' data from passing through ops. `deploy-from` is the other half: whoever controls ops could otherwise deploy code that copies your database. With both, a break-in at ops reads nothing and runs nothing of its own on your server.
