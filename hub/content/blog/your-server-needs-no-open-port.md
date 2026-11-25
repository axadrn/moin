---
title: "Your server needs no open port"
description: "Close every inbound port on your VPS and still deploy, SSH in and serve your apps. With Tailscale, a Cloudflare Tunnel and one firewall rule."
date: "2026-11-25"
---

Every open port is a door. Bots knock on all of them, all day. A fresh VPS sees its first SSH login attempts within minutes.

The safest door is the one that is not there. Most servers can close every inbound port and lose nothing.

## Inbound and outbound

A connection has a direction. **Inbound**: someone out there connects to your server. **Outbound**: your server connects out.

A firewall can block every inbound connection and still let your server reach out. Outbound is how it pulls images, sends mail and talks to its backups. Nobody can start a conversation with a server that only dials out.

## Step 1: SSH without port 22

[Tailscale](https://tailscale.com) puts your laptop and your server in one private network. Both dial out to Tailscale, so no port opens.

```bash
curl -fsSL https://tailscale.com/install.sh | sh
tailscale up --ssh
```

`--ssh` turns on Tailscale SSH. Now `ssh root@server-1` works from your laptop, over the private network. Keys and access live in your Tailscale admin, not in `authorized_keys`.

Check it works **before** the next step. Otherwise you lock yourself out.

## Step 2: close the doors

```bash
ufw default deny incoming
ufw default allow outgoing
ufw allow in on tailscale0
ufw enable
```

Every inbound port is closed now. Only the Tailscale network gets in.

Most hosts also have a firewall in front of the server, like Hetzner's Cloud Firewall. Use it as a second lock: no inbound rule at all.

## Step 3: visitors without port 80 and 443

Your apps still need visitors. A [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) does it the same way: `cloudflared` on your server dials out to Cloudflare, and Cloudflare sends the visitors through that connection.

```yaml
services:
  tunnel:
    image: cloudflare/cloudflared:latest
    command: tunnel --no-autoupdate run
    environment:
      TUNNEL_TOKEN: ${TUNNEL_TOKEN}
```

You get the token in the Cloudflare dashboard, under Zero Trust, Networks, Tunnels. There you also say which hostname goes to which container.

Don't want Cloudflare in the middle? Then open 80 and 443, and only those. They are the doors meant for the public.

## The trap: Docker skips your firewall

This one surprises people. A port you publish in Docker, like `ports: "5432:5432"`, is open to the internet **even with ufw on**. Docker writes its own iptables rules, and they come before ufw's.

So publish to localhost only:

```yaml
ports:
  - "127.0.0.1:5432:5432"
```

Or don't publish at all. Containers in one compose file reach each other by name, like `db:5432`, without any port.

## What you end up with

- No port 22. SSH over Tailscale.
- No database port. Containers talk inside Docker.
- No port for your tools. They dial out.
- Port 80 and 443 only if you don't use a tunnel.

## How moin does it

[moin](/) was built this way. Its daemon on your server dials out to the hub. The hub never connects in, and it holds no SSH key to your servers. Deploys, backups and logs all go over the line the daemon opened. Visitors come in over 80 and 443, or a Cloudflare Tunnel.

[How moin works](/docs/concepts/how-moin-works) has the whole picture.
