---
title: "Uptime"
description: "Every domain that serves something, visited once a minute from the outside"
group: "Every day"
order: 6
---

Uptime tells you whether your app is open. From the first deploy on, moin visits every domain that serves something once a minute, like a visitor. Nothing to set up.

- The main domain: the canonical one, or every domain of the main service with `canonical none` ([www or not](/docs/guides/domains#www-or-not)).
- The domains of your other services, like `api.my-app.example.com`.
- Not a domain that only redirects to the main one, like `www.my-app.example.com`.

Each domain has its own visitor. A domain that hangs never slows down another.

```surfaces
Web: the app, **Domains**, a domain under **Uptime**
CLI: moin uptime <app-name>
TUI: on the app, `u`; `r` visits again
API: GET /api/v1/apps/{app}/uptime
MCP: get_uptime
```

| State | Means |
| --- | --- |
| **up** | any answer below 500: a page, a redirect, a `404` or the password prompt of a protected app |
| **down** | two minutes without an answer: a `5xx`, no answer within 10 seconds, a refused connection, a name that does not resolve or a bad certificate |
| **pending** | not visited yet |
| **paused** | uptime of the domain is paused, or the app is stopped |

## Down and back up

A domain is down after two minutes without an answer, like a server. A shorter failure, like a restart, counts in the percentages but changes nothing.

A domain that goes down or comes back goes into the [activity](/docs/guides/activity) with the reason and how long it was down, every time.

It sends an [alert](/docs/guides/alerts) too, with a brake for a domain that flaps:

- A down alert at most once an hour per domain.
- A back-up alert only after a down alert.

## Change the path

The visit goes to the health path from `x-moin.health` ([Config in the repo](/docs/guides/config)), else `/`. Set another per domain. `default` goes back.

```surfaces
Web: the app, **Domains**, a domain under **Uptime**, its **Path**, then **Save**
CLI: moin uptime path my-app.example.com /health
TUI: on the app, `u`, pick the domain, `c`, type `/health`, then `enter`
API: PUT /api/v1/apps/{app}/uptime/{domain} {"path": "/health"}
MCP: set_uptime
```

## Pause uptime

For maintenance, or a domain whose DNS is not set yet. A paused domain counts neither as up nor as down.

```surfaces
Web: the app, **Domains**, a domain under **Uptime**, its **Visit every minute** switch
CLI: moin uptime pause my-app.example.com, then moin uptime resume my-app.example.com
TUI: on the app, `u`, pick the domain, then `p`
API: PUT /api/v1/apps/{app}/uptime/{domain} {"paused": true}
MCP: set_uptime
```

## Good to know

- The hosted hub visits only public addresses.
- Self-hosted, `UPTIME=false` turns the visits off. See [what is kept where](/docs/self-hosting#database-backup).
