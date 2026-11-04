---
title: "Domains and access"
description: "Your own domains with HTTPS, the www redirect and a password"
order: 4
---

Every server runs one proxy, Traefik, which ops starts with the first app. A proxy is the program that takes the web traffic and hands it to the right app. Traefik answers on ports 80 and 443, gets certificates from Let's Encrypt and sends HTTP to HTTPS. HTTPS is always on.

## Instant address

Every app answers at an address with HTTPS right after its first deploy, before you buy a domain:

```text
https://<app>.<server IPv4 with dashes>.sslip.io
https://shop.203-0-113-7.sslip.io
```

Services like [sslip.io](https://sslip.io) answer every such name with the IP in it, so it points at your server with no DNS to set. Let's Encrypt gives it a certificate like any domain. ops shows it on the app page, after `ops deploy` ("live at …"), in `ops apps <app>`, the TUI, the API and MCP.

A password guards it by default, so a fresh app is not open to the whole internet: the app's own [password](#protect-with-a-password) when it has one, else one ops made, user `ops`.

```surfaces
Web: the app page, **Instant address**, **Show password**
CLI: ops instant --show
TUI: on the app, `:instant`
API: GET /api/v1/apps/{app}/instant?show=true
MCP: get_instant
```

Pick another provider if one is down, take the password away, or turn the address off:

```surfaces
Web: the app page, **Instant address**, **Provider** or the **Password** switch
CLI: ops instant nip.io, ops instant open, ops instant protect, ops instant off
TUI: on the app, `:instant nip.io`, `:instant open`, `:instant protect` or `:instant off`
API: PUT /api/v1/apps/{app}/instant
MCP: set_instant
```

The providers are `sslip.io` (the default), `nip.io` and `traefik.me`. `x-ops.instant` in compose.yml [wins](#in-the-repo). Let's Encrypt issues at most 50 certificates a week for all of `traefik.me` together, so its HTTPS often waits for days; `sslip.io` and `nip.io` have no such shared limit.

An instant address needs a public IPv4 and ports 80 and 443 open to the internet, for Let's Encrypt and for visitors. A server behind a [Cloudflare Tunnel](/docs/guides/security#cloudflare-tunnel), or with no public IPv4, gets none: ops says so instead of the address. Give such an app a domain; ops points it at the tunnel. A quick tunnel or Tailscale Funnel for closed servers may come later.

## Add a domain

Puts your app on your own domain, with HTTPS.

1. Buy the domain anywhere, for example at Cloudflare or Porkbun.
2. Add it to the app. You can add several at once, such as the domain with and without `www.`:

```surfaces
Web: the app page, **Domains**, **Add domain**
CLI: ops domains add <domain> www.<domain>
TUI: on the app, `o`, then type the domain
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

3. Point each domain at the server. Open the DNS settings where you bought the domain and add an **A record**: its name is the domain, its value the server's IPv4 address. If the server has an IPv6 address, add an **AAAA record** with it too. ops shows both addresses and the record to set.

Changes apply right away: ops restarts the running release with the new routing, without a build. Before the first deploy they wait for it.

DNS takes a few minutes, sometimes longer. HTTPS starts once the domain points at the server: Traefik then gets a certificate by itself. Using a [Cloudflare Tunnel](/docs/guides/security#cloudflare-tunnel)? Then ops sets the DNS for you, as long as the domain is in your Cloudflare account.

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
DOMAIN          DNS            RESOLVES TO
<domain>        ● points here  203.0.113.5
www.<domain>    ○ set DNS
```

## Remove a domain

Takes a domain off the app. It stops answering for this app.

```surfaces
Web: the app page, **Domains**, the × next to the domain
CLI: ops domains rm <domain>
TUI: on the app, `o`, then type `-` and the domain, then `y`
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

The web, the CLI and the TUI ask first; `--yes` skips it in scripts. In the API and MCP, send the domains the app should keep. Remove the DNS record where you bought the domain too.

## www or not

Picks one domain as the main one. The others redirect to it for good, so visitors and search engines see one address.

```surfaces
Web: the app page, **Domains**, **Make canonical** on a domain
CLI: ops domains canonical <domain>
TUI: on the app, `o`, then type `=` and the domain
API: PUT /api/v1/apps/{app}/domains
MCP: set_domains
```

`ops domains canonical none` turns the redirect off. In the API and MCP, send the main domain as `canonical`.

## Protect with a password

Puts a username and password in front of an app, for example for a staging app or a preview. ops stores only a hash of the password, never the password itself.

```surfaces
Web: the app page, **Password**, **Protect with password**
CLI: ops protect --user <username>
TUI: on the app, `p`, then type `<username>:<password>`
API: PUT /api/v1/apps/{app}/protection
MCP: set_protection
```

The username is `ops` unless you pick another with `--user`. The CLI asks for the password on stdin. Visitors then see a sign-in box of their browser.

## Remove the password

Makes the app public again.

```surfaces
Web: the app page, **Password**, **Remove password**
CLI: ops protect off
TUI: on the app, `p`, type `off`, then `y`
API: DELETE /api/v1/apps/{app}/protection
MCP: remove_protection
```

A change of the domains or the password restarts the app so it applies. If nothing changes, like the same domains again or `protect off` on an app without a password, ops says "Nothing changed" and restarts nothing. The API answers with `"unchanged": true`.

## Domains per service

An app of several services, such as a `web` and an `api` in one compose.yml, can give each its own domains. The main service keeps the plain domains; the others name their service:

```surfaces
Web: the app page, **Domains**, the domain and its service, **Add domain**
CLI: ops domains add --service api api.<domain>
TUI: on the app, `o`, then type `api:api.<domain>`
API: PUT /api/v1/apps/{app}/domains with "service": "api"
MCP: set_domains with service
```

Each service gets its own route on its own port: `x-ops.ports` sets it, else ops reads the service's `ports` or `expose`, then 8080. The password and the canonical redirect work the same, and [uptime checks](/docs/guides/uptime) check every domain. A domain reaches one service only.

## In the repo

You can also keep domains and the password switch next to your code, in `compose.yml`:

```yaml
x-ops:
  domains: [<domain>, www.<domain>]
  canonical: <domain>
  protect: true   # the password itself is set in ops, never in the repo
  instant: nip.io # or sslip.io, traefik.me, off
```

For several services, `domains` takes a map by service, and `ports` their ports:

```yaml
x-ops:
  service: web
  domains:
    web: [<domain>]
    api: [api.<domain>]
  ports:
    api: 5678
```

Fields in `x-ops` win over the settings in ops. [Config in the repo](/docs/guides/config) lists all of them.

Once a deploy with `x-ops.domains` is live, every surface lists those domains, marked **from compose.yml**. ops cannot add or remove them then: change them in `compose.yml` and deploy. Trying it anyway in ops says so, with the API code `domains_from_compose`.

## Restart by hand

Domain and password changes restart the app by themselves. [Restart](/docs/guides/deploy#restart) starts the live release again by hand, with the env vars, domains and password as they are now, without a build.
