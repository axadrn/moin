---
title: "Domains and access"
description: "Your own domains with HTTPS, the www redirect and a password"
order: 4
---

Every server runs one proxy, Traefik, which ops starts with the first domain. It answers on ports 80 and 443, gets certificates from Let's Encrypt and sends HTTP to HTTPS. HTTPS is always on.

## Add a domain

1. Buy the domain anywhere, for example at Cloudflare or Porkbun.
2. Add it to the app:

| Web | CLI | TUI | MCP |
| --- | --- | --- | --- |
| app page, **Domains and access** | `ops domains add blog.de www.blog.de` | `o`, then `blog.de` | `set_domains` |

3. Point each domain at the server: an A record to its IPv4 and, when it has one, an AAAA record to its IPv6. ops shows both addresses, the record to set, and checks it; a domain that resolves to either address points here:

```text
DOMAIN        DNS            RESOLVES TO
blog.de       ● points here  203.0.113.5
www.blog.de   ○ set DNS
```

Changes apply right away: ops restarts the running release with the new routing, without a build. Before the first deploy they wait for it.

## www or not

Pick one domain as canonical; the others redirect to it with a permanent redirect.

| Web | CLI | TUI | MCP |
| --- | --- | --- | --- |
| **Make canonical** on a domain | `ops domains canonical blog.de` (or `none`) | `o`, then `=blog.de` | `set_domains` with `canonical` |

## Protect with a password

Put a username and password in front of an app, for a staging app or a preview. Only a bcrypt hash is stored.

| Web | CLI | TUI | MCP |
| --- | --- | --- | --- |
| **Password**, then **Protect** | `ops protect --user ops`, password on stdin | `p`, then `user:password` | `set_protection` |
| **Remove password** | `ops protect off` | `p`, then `off` | `remove_protection` |

## In the repo

```yaml
x-ops:
  domains: [blog.de, www.blog.de]
  canonical: blog.de
  protect: true   # the password itself is set in ops, never in the repo
```

Fields in `x-ops` win over the settings in ops.

## Apply by hand

`ops apply` (or `apply_app` in MCP) restarts the running release with the env vars, domains and password as they are now, without a build.
