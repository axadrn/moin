---
title: "Uptime"
description: "Every domain of every app checked once a minute from the outside"
group: "Every day"
order: 6
---

Uptime tells you whether your app is open: from the first deploy on, moin visits every domain of every app once a minute, like a visitor, and checks the status, the response time and the HTTPS certificate. Nothing to set up.

```surfaces
Web: the app, **Domains**, a domain under **Uptime**
CLI: moin uptime <app-name>
TUI: on the app, `u`; `r` checks again
API: GET /api/v1/apps/{app}/uptime
MCP: get_uptime
```

Per domain you see its state, the uptime over 24 hours, 7 and 30 days, the newest check, the average response time, when the certificate expires and the last outages.

| State | Means |
| --- | --- |
| **up** | any answer below 500: a page, a redirect, a `404` or the password prompt of a protected app |
| **down** | a `5xx`, no answer within 10 seconds, a refused connection, a name that does not resolve or a certificate a browser would refuse; tried again a few seconds later before it counts |
| **pending** | not checked yet, within a minute of the deploy |
| **paused** | the check is paused, or the app is stopped |

## Down and back up

A domain that goes down or comes back goes into the [activity](/docs/guides/activity) with the reason, such as `HTTP 502` or `no answer within 10s`, and how long it was down. A daemon that has not answered for two minutes puts `lost web-1` there, and `has web-1 back` when it connects again. Each one sends an [alert](/docs/guides/alerts).

## Change the path

The check goes to the health path from `x-moin.health` in `compose.yml` ([Config in the repo](/docs/guides/config)), else `/`. Set another per domain; `default` goes back.

```surfaces
Web: the app, **Domains**, a domain under **Uptime**, its **Path**, then **Save**
CLI: moin uptime path blog.de /health
TUI: on the app, `u`, pick the domain, `c`, type `/health`, then `enter`
API: PUT /api/v1/apps/{app}/uptime/{domain} {"path": "/health"}
MCP: set_uptime_check
```

## Pause a check

For maintenance, or a domain whose DNS is not set yet. A paused check counts neither as up nor as down, and starts over when it resumes.

```surfaces
Web: the app, **Domains**, a domain under **Uptime**, its **Check every minute** switch
CLI: moin uptime pause blog.de, then moin uptime resume blog.de
TUI: on the app, `u`, pick the domain, then `p`
API: PUT /api/v1/apps/{app}/uptime/{domain} {"paused": true}
MCP: set_uptime_check
```

## Good to know

- Each domain gets its own second of the minute, so the checks spread evenly.
- Checks per domain and hour stay 31 days in `history.db`, without a backup; every outage stays 90 days in the database, with its backup. See [what is kept where](/docs/self-hosting#database-backup).
- The hosted hub checks only public addresses.
- Self-hosted, checks run in production; `UPTIME_CHECKS=false` turns them off, `UPTIME_CHECKS=true` turns them on in development.
