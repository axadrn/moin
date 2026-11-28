---
title: "Domains and DNS"
description: "A domain is a name. DNS is the phone book that turns it into your server's address. A, AAAA and CNAME records, and the address moin gives you first."
group: "Basics"
order: 7
---

Your server has an address: `203.0.113.7`. Nobody wants to type that. People type a name: `my-app.example.com`.

DNS turns the name into the address. It is the phone book of the internet.

```text
browser: where is my-app.example.com?
   │
   ▼
DNS: my-app.example.com  A  203.0.113.7
   │
   ▼
browser ──▶ 203.0.113.7, your server
```

## A domain

You rent a domain, like `example.com`, at a registrar. Many sell them, and Cloudflare does too. A domain costs from about 10 to about 50 a year, by its ending.

Once it is yours, you make any name under it yourself: `my-app.example.com`, `www.example.com`, `api.example.com`. Those are subdomains. They cost nothing extra.

## Records

The phone book has lines. Each line is a record. You add them in the DNS settings, where you bought the domain, or at Cloudflare.

Three kinds are all you need:

| Record | Says | Example |
| --- | --- | --- |
| `A` | this name is at this IPv4 address | `my-app` → `203.0.113.7` |
| `AAAA` | this name is at this IPv6 address | `my-app` → `2001:db8::7` |
| `CNAME` | this name is another name | `www` → `my-app.example.com` |

- **A** is the main one. Every server has an IPv4 address.
- **AAAA** is the same for IPv6, the newer, longer addresses. If your server has one, add it too.
- **CNAME** points a name at another name. The browser then asks for that one. It can't stand on the bare domain, `example.com`, only on a name under it.

`@` in a DNS form means the bare domain itself.

## How long a change takes

Each record has a TTL, time to live. It says how long others may remember the answer. A new record is there within minutes. A change to an old one may take as long as its old TTL. Set it to 5 minutes while you set things up.

To check what the world sees:

```bash
dig +short my-app.example.com
```

It prints the address. Nothing? The record is not there yet.

## The instant address

DNS homework is no fun before you know the app runs. So moin gives every app an address that needs no DNS at all, right after the first deploy:

```text
✓ my-app v1 is live: https://my-app.203-0-113-7.sslip.io
```

[sslip.io](https://sslip.io) is a public DNS service. A name with an IP in it answers with that IP. `my-app.203-0-113-7.sslip.io` is at `203.0.113.7`. No record to add. HTTPS works on it too.

It needs your server's IPv4 address and ports 80 and 443. Use it to try things. Use your own domain for real visitors.

## Your own domain

```bash
moin domains add my-app my-app.example.com
```

```text
✓ my-app answers on https://my-app.example.com
  Point the DNS of my-app.example.com to my-server: an A record with its IP. The certificate follows by itself.
```

Then add the A record, `my-app` → your server's IP. The HTTPS certificate comes from Let's Encrypt by itself, once the name points at the server. You do nothing. [Domains](/docs/deploy#domains)

Behind a Cloudflare Tunnel it goes the other way. The domain points at Cloudflare, and the tunnel's Public Hostnames send it to your server. [Cloudflare Tunnel](/docs/basics/cloudflare-tunnel)

## If it does not work

- **The browser finds no server.** `dig +short` shows nothing, or an old address. Check the record. Wait for the TTL.
- **The browser warns about the certificate.** The name points at the server only since a moment. The certificate follows within a few minutes.
- **The bare domain should work too.** Add an A record for `@` as well, and `moin domains add my-app example.com`.
