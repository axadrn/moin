---
title: "Alerts"
description: "A mail when a site goes down, a server goes offline or a deploy fails"
order: 11
---

**The short version:** when something breaks, the owners and admins of the workspace get a mail. There is nothing to set up. Each person turns it off for themselves.

## What sends a mail

One mail per event, never one per minute:

- **A site goes down**, with the app, since when and the reason, such as `HTTP 502` or `no answer within 10s`. And one when it is **back up**, with how long it was down. See [Uptime](/docs/guides/uptime#down-and-back-up).
- **A server goes offline**: its agent has not answered for two minutes. The mail names the apps on it. And one when it is **back online**, with how long it was offline. See [Servers](/docs/guides/servers).
- **A deploy fails**, with its number and the error. A failed deploy leaves the live version running, or starts it again; the mail says which version that is. See [What a deploy does](/docs/guides/deploy#what-a-deploy-does).

Every mail has a button to the page in ops: the uptime of the app, the servers, or the app with its deploys.

## Who gets it

Every **owner** and **admin** of the workspace whose **Email me alerts** is on, at the address of their account. Members get none. Only a verified address gets alerts. It is on for everyone by default.

## Turn it off

For you, in every workspace:

```surfaces
Web: **Settings**, **Profile** tab, **Email me alerts**
CLI: ops alerts off, then ops alerts on; ops alerts shows it
TUI: settings tab, **Email me alerts**, `enter`, or `:alerts`
API: PUT /api/v1/account/email-alerts {"on": false}
MCP: set_email_alerts
```

`GET /api/v1/account` and the MCP tool `whoami` return it as `email_alerts`.

## What comes next

Alerts to Slack, Discord, Telegram and webhooks come next. See the [roadmap](/docs/roadmap).
