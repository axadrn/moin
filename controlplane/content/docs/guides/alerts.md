---
title: "Alerts"
description: "A mail, a Slack or Discord message, a Telegram message or a webhook when a site goes down, a server goes offline, a deploy fails, backups stop or a disk fills up"
order: 11
---

**The short version:** when something breaks, the owners and admins of the workspace get a mail. There is nothing to set up. Add Slack, Discord, Telegram or a webhook under **Alerts** to tell the team where it already is.

## What sends an alert

One alert per event, never one per minute:

- **A site goes down**, with the app, since when and the reason, such as `HTTP 502` or `no answer within 10s`. And one when it is **back up**, with how long it was down. See [Uptime](/docs/guides/uptime#down-and-back-up).
- **A server goes offline**: its agent has not answered for two minutes. The alert names the apps on it. And one when it is **back online**, with how long it was offline. See [Servers](/docs/guides/servers).
- **A deploy fails**, with its number and the error. A failed deploy leaves the live version running, or starts it again; the alert says which version that is. See [What a deploy does](/docs/guides/deploy#what-a-deploy-does).
- **Backups stop**: the continuous backup of a SQLite or Postgres database fails for three minutes, a snapshot cannot be uploaded, or WAL-G lost its place in the log. What is in your bucket stays; new changes are not there until it works again. And one when it does. See [Backups](/docs/guides/backups).
- **The daily restore check fails**: the newest backup did not restore cleanly. And one when a check passes again.
- **A disk is almost full**: the disk that holds the apps' data is over 90%. A full disk stops databases from writing. And one when it is under 85% again, so a disk at 88% does not alert back and forth.

Every alert links to the page in ops: the uptime of the app, the servers, the app with its deploys, or its backups. Mails and messages say the same:

```text
🔴 shop.example.com is down
App: shop
Since: 2026-11-03 14:05 UTC
Reason: HTTP 502
Open in ops
```

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

## Slack, Discord, Telegram and webhooks

Besides the mails, a workspace sends its alerts to up to 20 destinations. Each gets the events it is set for, all of them by default:

| Event | When |
| --- | --- |
| `uptime.down`, `uptime.up` | a site goes down, and is back up |
| `server.down`, `server.up` | a server goes offline, and is back online |
| `deploy.failed` | a deploy fails |
| `backup.failed`, `backup.recovered` | backups of a database stop, and work again |
| `drill.failed`, `drill.recovered` | the daily restore check fails, and passes again |
| `disk.full`, `disk.ok` | a server's disk is over 90%, and under 85% again |
| `update.available` | a newer release of an app from the [catalog](/docs/guides/catalog) is out, once per version |

A destination that got every event before new ones came gets the new ones too.

Every member sees the destinations. Only **owners** and **admins** add, test and remove them.

### Add a destination

```surfaces
Web: **Alerts** in the sidebar (`g` then `l`), **Add a destination**
CLI: ops alerts add slack team-ops, then the URL on stdin; ops alerts add telegram phone --chat -1001234567890, then the bot token; --events deploy.failed,uptime.down picks events
TUI: settings tab, `C` or `:alert`
API: POST /api/v1/alert-destinations {"kind": "slack", "name": "team-ops", "url": "https://hooks.slack.com/services/…"}
MCP: add_alert_destination
```

- **Slack**: an [incoming webhook](https://api.slack.com/messaging/webhooks) of the channel, `https://hooks.slack.com/services/…`.
- **Discord**: the channel's **Edit Channel**, **Integrations**, **Webhooks**, **New Webhook**, **Copy Webhook URL**, `https://discord.com/api/webhooks/…`. Messages never ping anyone.
- **Telegram**: make a bot with [@BotFather](https://t.me/BotFather) and add it to the chat. ops needs its token, like `123456:ABC-DEF…`, and the chat ID: a number like `-1001234567890` for a group or channel, or `@channelname` for a public channel.
- **Webhook**: any `http` or `https` URL of your own. See [Webhooks](#webhooks).

The URL and the bot token are stored encrypted, like your registry passwords, and never shown again in full: the lists show the host and the last characters, like `hooks.slack.com/…WXYZ`. They never go in the activity. The hosted control plane posts only to public addresses.

### Send a test

A test shows that the destination takes alerts, right after you add it or whenever you like:

```surfaces
Web: **Alerts**, **Send test** next to the destination
CLI: ops alerts test team-ops
TUI: settings tab, the destination, `T`, `enter` or `:test`
API: POST /api/v1/alert-destinations/team-ops/test
MCP: test_alert_destination
```

ops waits up to 10 seconds for the answer. A refused test says why, such as `HTTP 404: no_service`, and the API answers `424` with `alert_send_failed`.

### A destination that fails

Each destination gets 10 seconds per alert. One that does not take an alert is marked **failing**, with the reason, on every surface, and the activity says so once, not for every alert. The next alert or test that works clears it, and the activity says so too. Mails go out either way.

### Remove a destination

```surfaces
Web: **Alerts**, **Remove** next to the destination
CLI: ops alerts rm team-ops
TUI: settings tab, the destination, `x`, then `y`
API: DELETE /api/v1/alert-destinations/team-ops
MCP: remove_alert_destination
```

To change one, remove it and add it again.

## Webhooks

A webhook gets each alert as a JSON `POST`. Fields that do not belong to the event are empty:

```json
{
  "event": "uptime.down",
  "workspace": "acme",
  "app": "shop",
  "server": "web-1",
  "domain": "shop.example.com",
  "reason": "HTTP 502",
  "since": "2026-11-03T14:05:00Z",
  "duration": "",
  "url": "https://ops.example.com/app/acme/apps/shop/uptime"
}
```

`event` is one of the five above, or `test`. `reason` is why a site is down or the error of a failed deploy. `since` is when a site or server went down, `duration` how long it was down, like `4m30s`.

A new webhook comes with a **signing secret**, shown this once. Every post carries `X-Moin-Signature: sha256=` and the hex HMAC-SHA256 of the body with it. Until December 4, 2026 it carries the same as `X-Ops-Signature` too, the header's old name. Check it before you trust a post:

```go
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(body)
ok := hmac.Equal([]byte(r.Header.Get("X-Moin-Signature")), []byte("sha256="+hex.EncodeToString(mac.Sum(nil))))
```

Answer with any `2xx`; ops follows no redirects. A lost secret cannot be shown again: remove the webhook and add it again.
