---
title: "Domains"
description: "The instant address, your own domains with HTTPS, the www redirect and a password"
group: "Apps"
order: 2
---

A domain is the name people type to reach your app, like `my-app.example.com`. Every app gets one right after its first deploy: its instant address. Add your own when you are ready. HTTPS is always on.

```text
browser     opens my-app.example.com
   │
   ▼
DNS         my-app.example.com is
   │        203.0.113.7: your A record
   ▼
server      203.0.113.7, port 443
   │
   ▼
Traefik     HTTPS, finds the app
   │        by its domain
   ▼
app my-app  on its port
```

DNS is the phone book of the internet. It turns a name into the server's address.

## Add a domain

Add one or several at once, such as the domain with and without `www.`:

```surfaces
Web: the app page, **Domains**, **Add a domain**, then **Add**
CLI: moin domains add <domain> www.<domain>
TUI: on the app, `o`, `a`, then type the domain
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

Then, where you bought the domain, add an **A record**: the domain as the name, the server's IPv4 address as the value. If the server has IPv6, add an **AAAA record** too.

moin says whether the domain already points at the server. **Check** asks again later. Once DNS points at the server, the domain gets HTTPS by itself.

Behind a [Cloudflare Tunnel](/docs/guides/security#cloudflare-tunnel), moin sets the DNS for you when the domain is in your Cloudflare account.

A change to the domains, the password or the instant address waits for the next restart, start or deploy.

## Instant address

```text
https://<app>.<ip-dashes>.sslip.io
https://my-app.203-0-113-7.sslip.io
```

The name carries the server's IP, so it needs no DNS. A password guards it by default, user `moin`: the app's own [password](#protect-with-a-password) when it has one, else one moin made.

```surfaces
Web: the app, **Instant address**, **Show password**
CLI: moin instant --show
TUI: on the app, `o`: the instant address
API: GET /api/v1/apps/{app}/instant?show=true
MCP: get_instant
```

Switch the provider, drop the password, or turn the address off:

```surfaces
Web: the app, **Instant address**, **Provider** or the **Password** switch
CLI: moin instant nip.io, moin instant open, moin instant protect, moin instant off
TUI: on the app, `W`
API: PUT /api/v1/apps/{app}/instant
MCP: set_instant
```

The providers are `sslip.io` and `nip.io`. If one is down, switch to the other. The address needs a public IPv4 and ports 80 and 443 open. A server behind a tunnel gets none, and moin says so.

## Check the DNS

```surfaces
Web: the app page, **Domains**, **Check** next to the domain
CLI: moin domains
TUI: on the app, `o`
API: GET /api/v1/apps/{app}/domains
MCP: check_domains
```

```text
DOMAIN       DNS           RESOLVES TO
<domain>     ● points here 203.0.113.5
www.<domain> ○ set DNS
```

## Remove a domain

```surfaces
Web: the app, **Domains**, the × next to the domain
CLI: moin domains rm <domain>
TUI: on the app, `o`, pick the domain, `x`, then `y`
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

The API and MCP take the domains the app keeps. Remove the DNS record too.

## www or not

Pick the main domain. The others redirect to it for good.

```surfaces
Web: the app page, **Domains**, **Canonical**
CLI: moin domains canonical <domain>
TUI: on the app, `o`, pick the domain, then `c`
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

`moin domains canonical none` turns the redirect off. The API and MCP take it as `canonical`.

## Protect with a password

A username and password in front of the app, for staging or a preview. Visitors see their browser's sign-in box. moin stores only a hash.

```surfaces
Web: the app page, **Domains**, **Ask for a password**, then **Set password**
CLI: moin protect --user <username>
TUI: on the app, `o`, `p`, then type `<username>:<password>`
API: PUT /api/v1/apps/{app}/protection
MCP: set_protection
```

The username is `moin` unless you pick one. The CLI reads the password from stdin. To make the app public again:

```surfaces
Web: the app page, **Domains**, **Remove password**
CLI: moin protect off
TUI: on the app, `o`, `p`, then `y`
API: DELETE /api/v1/apps/{app}/protection
MCP: remove_protection
```

## Domains per service

An app with several services can give each one its own domains. The main service keeps the plain ones.

```surfaces
Web: the app page, **Domains**, the domain and its service, **Add**
CLI: moin domains add --service api api.<domain>
TUI: on the app, `o`, `a`, then type `api:api.<domain>`
API: PUT /api/v1/apps/{app}/domains with "service": "api"
MCP: set_domains with service
```

Each service gets its own port: `x-moin.ports`, else its `ports` or `expose`, then 8080. A domain reaches one service only.

## In the repo

Domains can live next to your code, in `compose.yml`:

```yaml
x-moin:
  domains: [<domain>, www.<domain>]
  canonical: <domain>
  protect: true   # the password itself is set in moin, never in the repo
  instant: nip.io # or sslip.io, off
```

For several services, `domains` takes a map and `ports` their ports:

```yaml
x-moin:
  service: web
  domains:
    web: [<domain>]
    api: [api.<domain>]
  ports:
    api: 5678
```

Once a deploy with `x-moin.domains` is live, those domains show **from compose.yml** and change only in the file. Trying it in moin says so (API code `domains_from_compose`). [Who decides](/docs/guides/config#who-decides).

## Good to know

- Each server runs one proxy, Traefik, started with the first app. It answers on ports 80 and 443 and gets certificates from Let's Encrypt.
- A change that changes nothing says "Nothing changed". The API answers `"unchanged": true`.
