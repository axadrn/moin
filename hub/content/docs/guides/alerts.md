---
title: "Alerts"
description: "A site goes down. A server goes offline. A deploy fails. Backups stop. A disk fills up. moin tells you by mail, Slack, Discord, Telegram or a webhook."
group: "Every day"
order: 7
---

When something breaks, moin tells you. The owners and admins of the workspace get a mail. Nothing to set up. Add Slack, Discord, Telegram or a webhook to tell the team where it already is.

```text
🔴 my-app.example.com is down
App: my-app
Since: 2026-11-03 14:05 UTC
Reason: HTTP 502
Open in moin
```

## What sends an alert

One alert per event, and one when it is fixed.

| Event | When |
| --- | --- |
| `uptime.down`, `uptime.up` | a site goes down, or comes back ([Uptime](/docs/guides/uptime#down-and-back-up)) |
| `server.down`, `server.up` | a daemon has not answered for two minutes; back online |
| `deploy.failed` | a deploy fails ([Deploy](/docs/guides/deploy#what-a-deploy-does)) |
| `backup.failed`, `backup.recovered` | a copy does not reach the bucket; and when it works again ([Backups](/docs/guides/backups)) |
| `check.failed`, `check.recovered` | the daily check could not restore the newest copy; passes again |
| `disk.full`, `disk.ok` | a server's data disk is over 90%; under 85% again |
| `update.available` | a newer version of a [catalog](/docs/guides/catalog) app is out |
| `server.daemon_update_failed` | a daemon could not update by itself ([Servers](/docs/guides/servers#updates)) |

## Who gets it

Every **owner** and **admin** whose **Email me alerts** is on. On by default.

```surfaces
Web: **Settings**, **Account**, **Email me alerts**
CLI: moin alerts off, then moin alerts on; moin alerts shows it
TUI: opens the web: `w`, then `s`
API: PUT /api/v1/account/email-alerts {"on": false}
MCP: set_email_alerts
```

## Slack, Discord, Telegram and webhooks

A workspace sends its alerts to up to 20 destinations, each with all events or the ones you pick.

### Add a destination

```surfaces
Web: **Settings**, **Alerts** (`g` then `l`), **Add a destination**
CLI: moin alerts add slack team-chat, then the URL on stdin; moin alerts add telegram phone --chat -1001234567890, then the bot token; --events deploy.failed,uptime.down picks events
TUI: opens the web: `w`, then `n`
API: POST /api/v1/alert-destinations {"kind": "slack", "name": "team-chat", "url": "https://hooks.slack.com/services/…"}
MCP: add_alert_destination
```

| Kind | What it needs |
| --- | --- |
| **Slack** | an [incoming webhook](https://api.slack.com/messaging/webhooks) of the channel |
| **Discord** | the channel's **Edit Channel**, **Integrations**, **Webhooks**, **New Webhook**, **Copy Webhook URL** |
| **Telegram** | a bot from [@BotFather](https://t.me/BotFather) in the chat: its token and the chat ID, such as `-1001234567890` |
| **Webhook** | any `http` or `https` URL of your own, see [Webhooks](#webhooks) |

### Send a test

```surfaces
Web: **Settings**, **Alerts**, **Send test** next to the destination
CLI: moin alerts test team-chat
TUI: opens the web: `w`, then `n`
API: POST /api/v1/alert-destinations/team-chat/test
MCP: test_alert_destination
```

A refused test says why. The API answers `424` with `alert_send_failed`.

### Remove a destination

```surfaces
Web: **Settings**, **Alerts**, **Remove** next to the destination
CLI: moin alerts rm team-chat
TUI: opens the web: `w`, then `n`
API: DELETE /api/v1/alert-destinations/team-chat
MCP: remove_alert_destination
```

To change one, remove it and add it again.

## Webhooks

Each alert comes as a JSON `POST`. Fields that do not belong to the event are empty:

```json
{
  "event": "uptime.down",
  "workspace": "acme",
  "app": "my-app",
  "server": "web-1",
  "domain": "my-app.example.com",
  "reason": "HTTP 502",
  "since": "2026-11-03T14:05:00Z",
  "duration": "",
  "url": "https://moin.example.com/app/acme/apps/my-app/uptime"
}
```

A new webhook comes with a **signing secret**, shown once. Every post carries `X-Moin-Signature: sha256=` and the hex HMAC-SHA256 of the body. Check it:

```go
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(body)
ok := hmac.Equal([]byte(r.Header.Get("X-Moin-Signature")), []byte("sha256="+hex.EncodeToString(mac.Sum(nil))))
```

Answer with any `2xx`. moin follows no redirects. A lost secret cannot be shown again: remove the webhook and add it again.

## Good to know

- Only owners and admins add, test and remove destinations.
- A failing destination is marked **failing** with the reason. The next one that works clears it. Mails go out either way.
- URLs and bot tokens are stored encrypted and never shown again in full.
- The hosted hub posts only to public addresses.
