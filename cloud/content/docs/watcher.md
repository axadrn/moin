---
title: "Watcher"
description: "A dead server can't tell you it's dead. A watcher on another machine notices, and alerts you. Set one up in four commands."
group: "Servers and data"
order: 3
---

A dead server can't tell you it's dead. Someone else has to notice. That is the watcher.

Your server sends it a short report every minute. When the reports stop, the watcher alerts you. It also visits your domains from outside, like a visitor.

## What it knows

Whatever the daemons report. Nothing to set up.

- your servers, and the version of moin on each
- their apps, and the release that runs
- the domains of each app
- the backups, and the last restore check

It has one small page that shows it all: what is fine, what needs you, the outages. Nothing on it changes a server.

## Alerts

Two kinds of alerts, from two places. Each problem alerts once when it starts, and once when it is over.

| Who alerts | What |
| --- | --- |
| the daemon, on your server, while it lives | a backup that fails, a copy that did not restore, an app that keeps crashing, a disk that is 90% full, no watcher reachable |
| the watcher, from outside | a server that stopped reporting, a domain that is down from outside |

The watcher's alerts read like this:

```text
prod stopped reporting
prod reports again
shop.example.com is down
shop.example.com is up again
```

- **Silent**: three reports in a row are missing. With a report every minute, that is three minutes.
- **Down**: three visits in a row fail, one a minute. A visit fails on a 5xx, a timeout, a refused connection or a bad certificate. Any other answer, a redirect too, is up.
- A domain of a silent server does not alert on its own. The silence says more.

Alerts go to Telegram, Slack, Discord or a webhook. The server's own alerts are set with `moin alerts`. [CLI](/docs/cli#the-team-and-alerts)

## Where it runs

Never on the server it watches. When that server dies, the watcher dies with it, and nobody tells you.

- **A small second server.** The cheapest one your provider has is enough.
- **Two servers that watch each other.** Best at two different providers.
- **moin cloud.** We run one for you. Coming. [moin cloud](/docs/cloud)

A server can report to several watchers.

## Set one up

Four steps. The first three on the watcher's machine, the last on your laptop.

**1. Install moin** on the watcher's machine, as on your laptop:

```bash
curl -fsSL https://moin.run/cli.sh | sh
```

**2. Make a token** for the server it watches. Name the server as you deploy to it, like `prod`. `--url` is where your servers reach this watcher. Leave it out, and moin asks.

```bash
moin watcher token prod --url https://watcher.example.com
```

The first time, there is no config yet. It prints one, and the line for your laptop:

```text
1. Save this as /etc/moin/watcher.yml, fill in alerts:, then run: chmod 600 /etc/moin/watcher.yml

password: <a new password for the page>
servers:
  prod: <a new token>
alerts:
  - kind: telegram
    token: <bot token>
    chat_id: <chat id>

2. On your laptop, point prod at this watcher:

  moin watcher connect prod https://watcher.example.com <a new token>
```

Do what it says: save the file, fill in `alerts:`, then `chmod 600`.

**3. Start the watcher:**

```bash
moin watcher run
```

It listens on port 8080 and runs until you stop it. Keep it running as a service, like any program. With systemd, save this as `/etc/systemd/system/moin-watcher.service`:

```ini
[Unit]
Description=moin watcher
After=network-online.target

[Service]
ExecStart=/usr/local/bin/moin watcher run
Restart=always

[Install]
WantedBy=multi-user.target
```

Then `systemctl enable --now moin-watcher`.

**4. On your laptop**, paste the line from step 2:

```bash
moin watcher connect prod https://watcher.example.com <token>
```

```text
✓ prod reports to https://watcher.example.com every minute
```

That's it. `moin servers` shows it in the column `WATCHERS`.

## One more server

On the watcher, make a token for it:

```bash
moin watcher token web-1 --url https://watcher.example.com
```

The config is there now, so it prints one line for it:

```text
1. Put this line under servers: in /etc/moin/watcher.yml, then restart the watcher:

  web-1: <a new token>

2. On your laptop, point web-1 at this watcher:

  moin watcher connect web-1 https://watcher.example.com <a new token>
```

The watcher reads its config when it starts. So restart it after a change: `systemctl restart moin-watcher`.

## Its config

`/etc/moin/watcher.yml`, three keys:

```yaml
# The password of the page. At least 16 characters.
password: <a long password>

# Each server it watches, and the token that server reports with.
# moin watcher token makes one. At least 16 characters, each its own.
servers:
  prod: <token>
  web-1: <token>

# Where the watcher's alerts go. At least one.
alerts:
  - kind: telegram
    token: <bot token>
    chat_id: <chat id>
  - kind: discord        # slack, discord or webhook: each takes a url
    url: https://discord.com/api/webhooks/<your hook>
```

- Only its owner may read it: `chmod 600`. Else the watcher does not start.
- A key it does not know is an error.
- A server's name here must be the server's own name, `name:` in its [config](/docs/config). The first deploy sets that to your SSH alias.

`moin watcher run` takes three flags:

| Flag | Default | What |
| --- | --- | --- |
| `--listen` | `:8080` | the address it listens on |
| `--config` | `/etc/moin/watcher.yml` | its config |
| `--dir` | `/var/lib/moin/watcher` | where it keeps what it knows |

`moin watcher token` takes `--config` too.

## How servers reach it

The watcher is the only part of moin that listens. Two ways to reach it:

- **Over Tailscale.** Put the watcher in the same tailnet. It needs no public port. The URL is `http://<watcher's tailnet name>:8080`. Tailscale encrypts the traffic.
- **Over HTTPS.** Put a proxy with HTTPS in front of port 8080, and use `https://watcher.example.com`.

Each server reports with its own token. A report with a token the watcher does not know gets `401`. A report is all it takes in: the answer is a status code, and the daemon acts on nothing in it.

## The page

Open the watcher's URL in a browser. It asks for a password: any user name, and `password:` from its config.

It shows what needs you first, then each server with its apps, their domains as seen from outside, and their databases. It loads itself again every minute.

## See and change where a server reports

```bash
moin servers
moin watcher connect prod https://watcher.example.com <token>
moin watcher disconnect prod https://watcher.example.com
```

`moin watcher disconnect` asks first, because that watcher will then say the server is silent. In a script, add `-y`.

## If it is hacked

Someone who takes over a watcher sees your server and app names, your domains, and when backups ran. They can silence its alerts. They can't reach any server: the daemon acts on nothing a watcher answers. Run two watchers, and one hacked watcher can't silence you.

## If it breaks

- **The watcher is down.** Your servers keep running and keep their backups. After five reports no watcher took, each daemon alerts: `no watcher is reachable`. With two watchers, the other one still alerts.
- **A server is silent.** The watcher alerts once. When the server reports again, it says it is back.
- **The watcher was off for a while.** That time does not count as silence. It starts counting again when it starts.
