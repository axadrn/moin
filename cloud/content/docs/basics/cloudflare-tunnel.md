---
title: "Cloudflare Tunnel"
description: "Visitors reach your app, and your server has no open web port. How a Cloudflare Tunnel works, what Cloudflare sees, and when to use one."
group: "Basics"
order: 6
---

Your app needs visitors. Visitors need a door. Usually that is port 80 and 443, open to the whole internet.

A Cloudflare Tunnel turns that around. Your server calls Cloudflare. Visitors come to Cloudflare. Cloudflare passes them down the line your server opened.

```text
visitor ──▶ Cloudflare ◀════ tunnel ════ your server
             HTTPS here     the server   no open port
                            dials out
```

A door that only opens from the inside. Nobody can knock on it.

## How it works

1. A small program, `cloudflared`, runs on your server.
2. It dials out to Cloudflare and keeps that line open.
3. Your domain points at Cloudflare, not at your server.
4. A visitor asks for `my-app.example.com`. Cloudflare sends the request down the line.
5. Your app answers the same way back.

Your server never accepts a connection from the internet. So ports 80 and 443 can close. Bots that scan your IP find nothing at all.

## With moin

```bash
moin secure my-server
```

It closes public SSH, then asks for the token of a tunnel. Make one in the Cloudflare dashboard: Networks, Tunnels, Create a tunnel, Cloudflared. The token is the long text after `--token`.

Then, in the tunnel's Public Hostnames, send each domain to:

```text
http://moin-traefik:8080
```

That is moin's proxy on your server. It knows which app answers which domain.

After that, no public TCP port is left on the server. The firewall drops the rest. [Cloudflare Tunnel and the firewall](/docs/security#cloudflare-tunnel-and-the-firewall)

The web comes into the tunnel only once it reaches Cloudflare. With a wrong token, it stays where it was. To take the web out again:

```bash
moin secure my-server --tunnel off
```

It comes on ports 80 and 443 again.

## What Cloudflare sees

This is the trade. Be clear about it.

- Cloudflare serves HTTPS for your domain. So it opens each request, and sees it in plain text: pages, forms, cookies, logins.
- It sees who visits, and when.
- If Cloudflare is down, your app is unreachable, even when your server is fine.

In return, your server has no open door. And Cloudflare stops a lot of junk before it reaches you.

## What you need

- A Cloudflare account. The free plan works.
- Your domain in it. Cloudflare must run the DNS of the domain. [Domains and DNS](/docs/basics/dns)
- Your own domain for the app. The instant address, `*.sslip.io`, needs ports 80 and 443, so it does not work behind the tunnel.

## When to use it

Use it when:

- you want no open port at all
- you already have your domain at Cloudflare
- you are fine with Cloudflare seeing your traffic

Skip it when:

- your app handles data that no third party may see
- you need something that is not plain web, like a game server or a mail server
- you want the instant address while you try things

Without a tunnel, moin keeps 80 and 443 open, and nothing else. That is a fine place to be too. HTTPS comes from Let's Encrypt, on your server, and nobody in between sees your traffic.
