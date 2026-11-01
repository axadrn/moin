---
title: "Domains and access"
description: "Your own domains with HTTPS, the www redirect and a password"
order: 4
---

Every server runs one proxy, Traefik, which ops starts with the first domain. A proxy is the program that takes the web traffic and hands it to the right app. Traefik answers on ports 80 and 443, gets certificates from Let's Encrypt and sends HTTP to HTTPS. HTTPS is always on.

## Add a domain

Puts your app on your own domain, with HTTPS.

1. Buy the domain anywhere, for example at Cloudflare or Porkbun.
2. Add it to the app:

```surfaces
Web: the app page, **Domains**, **Add domain**
CLI: ops domains add blog.de www.blog.de
TUI: on the app, `o`, then type `blog.de`
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

3. Point each domain at the server: an A record to its IPv4 address and, when it has one, an AAAA record to its IPv6 address. ops shows both addresses and the record to set.

Changes apply right away: ops restarts the running release with the new routing, without a build. Before the first deploy they wait for it.

## Check the DNS

Shows for each domain whether it already points at the server. DNS is the internet's address book: it says which server a domain belongs to.

```surfaces
Web: the app page, **Domains**
CLI: ops domains
TUI: on the app, `o`
API: GET /api/v1/apps/{app}/domains
MCP: check_domains
```

A domain that resolves to either address of the server points here:

```text
DOMAIN        DNS            RESOLVES TO
blog.de       ● points here  203.0.113.5
www.blog.de   ○ set DNS
```

## www or not

Picks one domain as the main one. The others redirect to it for good, so visitors and search engines see one address.

```surfaces
Web: the app page, **Domains**, **Make canonical** on a domain
CLI: ops domains canonical blog.de
TUI: on the app, `o`, then type `=blog.de`
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

`ops domains canonical none` turns the redirect off. In the API and MCP, send the main domain as `canonical`.

## Protect with a password

Puts a username and password in front of an app, for example for a staging app or a preview. ops stores only a hash of the password, never the password itself.

```surfaces
Web: the app page, **Password**, **Protect with password**
CLI: ops protect --user ops
TUI: on the app, `p`, then type `user:password`
API: PUT /api/v1/apps/{app}/protection
MCP: set_protection
```

The CLI asks for the password on stdin.

## Remove the password

Makes the app public again.

```surfaces
Web: the app page, **Password**, **Remove password**
CLI: ops protect off
TUI: on the app, `p`, then type `off`
API: DELETE /api/v1/apps/{app}/protection
MCP: remove_protection
```

## In the repo

You can also keep domains and the password switch next to your code, in `compose.yml`:

```yaml
x-ops:
  domains: [blog.de, www.blog.de]
  canonical: blog.de
  protect: true   # the password itself is set in ops, never in the repo
```

Fields in `x-ops` win over the settings in ops. [Config in the repo](/docs/guides/config) lists all of them.

## Restart by hand

Domain and password changes restart the app by themselves. [Restart](/docs/guides/deploy#restart) starts the live release again by hand, with the env vars, domains and password as they are now, without a build.
