---
title: "Watcher"
description: "A dead server can't tell you it's dead. A watcher on another server notices, and alerts you. Run your own, or let moin cloud run one."
group: "Servers and data"
order: 3
---

A dead server can't tell you it's dead. Someone else has to notice. That is the watcher.

Your server sends it a short report, again and again. When the reports stop, the watcher alerts you.

## What it knows

Whatever the daemons report. Nothing to set up.

- your servers and their apps
- the domains of each app
- the backups, and the last restore check that proved one

It has one small page that shows it all: what is green, the last proven restore, the outages. Nothing on it changes a server.

## Alerts

Two kinds of alerts, from two places. Each event alerts once, and again when it is over.

| Who alerts | What |
| --- | --- |
| the daemon, on your server, while it lives | a backup that failed, a restore check that failed, an app that crashes, a full disk |
| the watcher, from outside | a server that stopped reporting, a domain that is down from outside |

Alerts go to Telegram, Slack, Discord or a webhook.

Set the server's own alerts:

```bash
moin alerts add prod telegram --token <bot token> --chat-id <chat id>
moin alerts add prod slack --url <webhook url>
moin alerts test prod
moin alerts prod
```

Discord and a webhook take `--url` too.

The watcher's alerts are in its config, below.

## Where it runs

Never on the server it watches. When that server dies, the watcher dies with it, and nobody tells you.

- **A small second server.** The cheapest one your provider has is enough.
- **Two servers that watch each other.** Best at two different providers.
- **moin cloud.** We run one for you. Coming. [moin cloud](/docs/cloud)

A server can report to several watchers.

## Run one

On the watcher's server, install moin as on your laptop:

```bash
curl -fsSL https://moin.example.com/cli.sh | sh
```

Write its config, `/etc/moin/watcher.yml`:

```yaml
password: <a long password for its page>
alerts:
  - kind: telegram
    token: <bot token>
    chat_id: <chat id>
```

Start it:

```bash
moin watcher run
```

Then, on the watcher, make a token for each server it watches:

```bash
moin watcher token prod
```

It prints the next steps, with the exact line for your laptop:

```bash
moin watcher connect prod <url> <token>
```

Run that on your laptop. From then on, `prod` reports to this watcher.

It listens on port 8080. Its state lives in `/var/lib/moin/watcher`.

## How servers reach it

The watcher is the only part of moin that listens. Two ways to reach it:

- **Over Tailscale.** Put the watcher in the same tailnet. It needs no public port. The URL is `http://<watcher's tailnet name>:8080`. Tailscale encrypts the traffic.
- **Over HTTPS.** Put a proxy with HTTPS in front of port 8080, and use `https://watcher.example.com`.

Each server reports with its own token. A report with a token the watcher does not know gets `401`.

## See and change where a server reports

`moin servers` shows the watchers of each server.

```bash
moin watcher connect prod https://watcher.example.com <token>
moin watcher disconnect prod https://watcher.example.com
```

## If it is hacked

Someone who takes over a watcher sees your server and app names, your domains, and when backups ran. They can silence its alerts. They can't reach any server: the daemon acts on nothing a watcher answers. Run two watchers, and one hacked watcher can't silence you.

## If it breaks

- **The watcher is down.** Your servers keep running and keep their backups. With two watchers, the other one still alerts.
- **A server is silent.** The watcher alerts once. When the server reports again, it says it is back.
