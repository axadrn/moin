---
title: "Uptime"
description: "Every domain of every app checked once a minute from the outside, with nothing to set up"
order: 11
---

From the first deploy on, the control plane visits every domain of every app once a minute, the way a visitor would: an HTTPS `GET`, the status code, how long the answer took and the HTTPS certificate. There is nothing to set up and no second tool to run. A self-hosted control plane does the same.

- **Up** is any answer below 500: a page, a redirect, a `404` or the password prompt of a protected app all mean the app answers.
- **Down** is a `5xx`, no answer within 10 seconds, a refused connection, a DNS name that does not resolve or a certificate a browser would refuse. A failed check is tried again a few seconds later; only when that fails too is the domain down, so one lost packet is no outage.
- **Where it goes**: the health path from `x-moin.health` in `compose.yml` ([Config in the repo](/docs/guides/config)), else `/`. You can change it per domain.

Each domain gets its own second of the minute, so the checks of many apps spread evenly instead of all starting at once.

## See the uptime

Per domain: the state, uptime over the last 24 hours, 7 and 30 days, the newest check with its status and response time, the average response time, when the certificate expires and the last outages.

```surfaces
Web: the app, **Uptime** tab
CLI: moin uptime <app-name>
TUI: on the app, `u`; `r` checks again
API: GET /api/v1/apps/{app}/uptime
MCP: get_uptime
```

A domain shows **pending** until its first check, within a minute of the deploy. A stopped app is not checked; its domains show **paused** until it runs again.

## Down and back up

When a domain goes down and when it comes back, moin writes it to the [activity log](/docs/guides/activity), with the reason, such as `HTTP 502` or `no answer within 10s`, and how long it was down. The outage shows under the domain too.

A dead server cannot warn anyone itself, so the control plane watches the agents too: an agent that has not answered for two minutes puts `lost web-1` in the activity, and `has web-1 back` when it connects again. The uptime of its apps' domains shows the outage from the outside.

The owners and admins of the workspace get a mail for each of these, see [Alerts](/docs/guides/alerts).

## Change the path

Checks a path like `/health` instead of the health path or `/`. `default` goes back.

```surfaces
Web: the app, **Uptime**, the domain's **Path**, then **Save path**
CLI: moin uptime path blog.de /health
TUI: on the app, `u`, pick the domain, `c`, type `/health`, then `enter`
API: PUT /api/v1/apps/{app}/uptime/{domain} {"path": "/health"}
MCP: set_uptime_check
```

## Pause a check

For maintenance, or a domain whose DNS is not set yet. A paused check counts neither as up nor as down, and starts over when it resumes.

```surfaces
Web: the app, **Uptime**, the domain's **Uptime check** switch
CLI: moin uptime pause blog.de, then moin uptime resume blog.de
TUI: on the app, `u`, pick the domain, then `p`
API: PUT /api/v1/apps/{app}/uptime/{domain} {"paused": true}
MCP: set_uptime_check
```

## What moin keeps

The newest result of each domain lives in the control plane's memory. Per domain and hour, how many checks ran, how many found it up and their response times stay for 31 days, in `history.db` without a backup: they grow back. Every outage stays for 90 days in the database, with its backup. See [what is kept where](/docs/self-hosting#database-backup). On the hosted control plane checks only reach public addresses.

A self-hosted control plane checks in production; `UPTIME_CHECKS=false` turns the checks off, `UPTIME_CHECKS=true` turns them on in development.

A small server checks about 100,000 domains a minute: each one at its own second, so they never all go at once.
