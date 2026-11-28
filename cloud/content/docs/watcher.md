---
title: "Watcher"
description: "A dead server can't tell you it's dead. A watcher on another machine notices, and alerts you. Set one up in four commands."
group: "Servers and data"
order: 3
---

A dead server can't tell you it's dead. Someone else has to notice. That is the watcher.

Your server sends it a short report every minute. When the reports stop, the watcher alerts you. It also visits your domains from outside, like a visitor.

## Alone, or with a watcher

Your server watches itself while it lives. A watcher tells you when it doesn't.

Every setup: deploys, backups restored every day, alerts while your server lives, email alerts through your own mail server.

| | No watcher | Self-hosted | moin cloud |
| --- | --- | --- | --- |
| You hear when your server dies | no | yes | yes |
| Uptime checks every minute, instead of UptimeRobot | no | yes | yes |
| What happened, on your phone | no | yes | yes |
| A copy a hacked server can't delete | [lock your bucket](/docs/backups#lock-your-bucket) | [lock your bucket](/docs/backups#lock-your-bucket) | yes, built in |
| Off-site copies without a bucket to set up | no | no | yes |
| Email alerts without your own mail server | no | no | yes |
| Nothing to run, update or watch | yes | no | yes |

Your own watcher is free and open source: run it on a small second server. moin cloud runs one for you, from $7 a month: `moin watcher connect my-server` and it is on.

## What it knows

Whatever the daemons report. Nothing to set up.

- your servers, and the version of moin on each
- their apps, and the release that runs
- the domains of each app
- the backups, and the last restore check
- the knocks of the day: connection attempts the firewall dropped

It has one small page that shows it all: what is fine, what needs you, the outages. Nothing on it changes a server.

## Alerts

Two kinds of alerts, from two places. Each problem alerts once when it starts, and once when it is over.

| Who alerts | What |
| --- | --- |
| the daemon, on your server, while it lives | a backup that fails, a copy that did not restore, a restore check that is late, a bucket that takes no backups, an app that keeps crashing, a disk that is 90% full, a mistake in the config, Tailscale that does not run or a key that expires soon, a deploy of a followed branch that fails, no watcher reachable |
| the watcher, from outside | a server that stopped reporting, a domain that is down from outside |

The watcher's alerts read like this:

```text
my-server stopped reporting
my-server reports again
my-app.example.com is down
my-app.example.com is up again
```

- **Silent**: three reports in a row are missing. With a report every minute, that is three minutes.
- **Down**: three visits in a row fail, one a minute. A visit fails on a 5xx, a timeout, a refused connection or a bad certificate. Any other answer, a redirect too, is up.
- A domain of a silent server does not alert on its own. The silence says more.

Alerts go to Telegram, Slack, Discord, a webhook or an email. Set them once per server, and they go everywhere:

```bash
moin alerts add my-server telegram --token <bot token> --chat-id <chat id>
moin alerts add my-server email --to ada@example.com --smtp smtp.example.com:587 --user alerts@example.com
```

An email goes through your own SMTP server. moin asks for the password and does not show it. In a script, pipe it in. The SMTP server must offer STARTTLS, most do on port 587, or speak TLS from the start, as on port 465. Without TLS nothing is sent, so the password never travels in the clear. Is the user not an address? Add `--from alerts@example.com`.

- The daemon on my-server alerts there while it lives.
- Each report carries them to my-server's watchers. So a watcher alerts there too, when my-server goes silent or a domain is down.
- Your own watcher and moin cloud do the same. moin cloud also sends a mail to your account.
- A report only goes over HTTPS, or inside your tailnet. So the tokens never travel in the clear.
- A watcher sends a server's alerts only to public addresses: a webhook over `https://`, an email through a public SMTP server. It follows no redirect. So a hacked server can't make it knock on its own network.
- A webhook or an SMTP server inside your tailnet? Put it in the watcher's own [config](#its-config). What you wrote there goes anywhere.

[CLI](/docs/cli#the-team-and-alerts)

## Where it runs

Never on the server it watches. When that server dies, the watcher dies with it, and nobody tells you.

- **A small second server.** The cheapest one your provider has is enough.
- **Two servers that watch each other.** Best at two different providers.
- **moin cloud.** We run one for you: `moin watcher connect my-server`. [moin cloud](/docs/cloud)

One watcher watches all your servers. A server can also report to two watchers, for safety.

## Set one up

Four steps. The first three on the watcher's machine, the last on your laptop.

**1. Install moin** on the watcher's machine, as on your laptop:

```bash
curl -fsSL https://moin.run/cli.sh | sh
```

**2. Make a token** for the server it watches. Name the server as you deploy to it, like `my-server`. `--url` is where your servers reach this watcher. Leave it out, and moin asks.

```bash
moin watcher token my-server --url https://watcher.example.com
```

The first time, there is no config yet. It prints one, and the line for your laptop:

```text
1. Save this as /etc/moin/watcher.yml, then run: chmod 600 /etc/moin/watcher.yml

password: <a new password for the page>
servers:
  my-server: <a new token>

2. On your laptop, point my-server at this watcher:

  moin watcher connect my-server https://watcher.example.com <a new token>

3. Alerts go where you send them, on your laptop:

  moin alerts add my-server telegram
```

Do what it says: save the file, then `chmod 600`. The alerts come from my-server itself: `moin alerts add my-server …`.

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
moin watcher connect my-server https://watcher.example.com <token>
```

```text
✓ my-server reports to https://watcher.example.com every minute
```

That's it. `moin servers` shows it in the column `WATCHERS`, and `moin watcher` too:

```text
SERVER      WATCHERS
my-server   https://watcher.example.com
```

## One more server

On the watcher, make a token for it:

```bash
moin watcher token staging --url https://watcher.example.com
```

The config is there now, so it prints one line for it:

```text
1. Put this line under servers: in /etc/moin/watcher.yml, then restart the watcher:

  staging: <a new token>

2. On your laptop, point staging at this watcher:

  moin watcher connect staging https://watcher.example.com <a new token>

3. Alerts go where you send them, on your laptop:

  moin alerts add staging telegram
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
  production: <token>
  staging: <token>

# Optional: the watcher's own alerts. They get every alert too.
# For a server that set no alerts, or runs an older moin, they are all there is.
alerts:
  - kind: telegram
    token: <bot token>
    chat_id: <chat id>
  - kind: discord        # slack, discord or webhook: each takes a url
    url: https://discord.com/api/webhooks/<your hook>
  - kind: email
    to: ada@example.com
    smtp: smtp.example.com:587
    user: alerts@example.com
    password: <the password of that user>
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

One exception, for moin cloud: after a copy, the daemon asks each watcher whether it takes the copy. Your own watcher answers `404`, so nothing happens. moin cloud, for a paying workspace, answers with one address to upload that copy to. The daemon uses it only over https, only to a public address, for one PUT, and follows no redirect. [moin cloud](/docs/cloud#a-copy-your-server-cant-delete)

## The page

Open the watcher's URL in a browser. It asks for a password: any user name, and `password:` from its config.

It shows what needs you first, then each server with its apps, their domains as seen from outside, and their databases. It loads itself again every minute.

## See and change where a server reports

```bash
moin watcher                                                    # each server's watchers
moin watcher connect my-server https://watcher.example.com <token>
moin watcher disconnect my-server https://watcher.example.com
moin watcher connect my-server                                  # moin cloud
moin watcher disconnect my-server                               # moin cloud
```

Without an address, it is moin cloud. The first time, the browser signs your laptop in. `moin watcher` shows your plan of moin cloud too, under the table:

```text
SERVER       WATCHERS
production   moin cloud
staging      none: nobody notices when it is gone

moin cloud  ada@example.com  Trial, 12 days left
```

A server with no watcher is yellow in `moin` too. Without a plan to pay, the trial days left and the billing link show here.

`moin watcher disconnect` asks first, because that watcher will then say the server is silent. In a script, add `-y`.

## If it is hacked

Someone who takes over a watcher sees your server and app names, your domains, and when backups ran. They get your alert channels, so they can post to them, and send mail as your SMTP user. Make a new bot token, webhook or SMTP password, and `moin alerts add` it. They can silence its alerts. They can't reach any server: the daemon acts on nothing a watcher answers, but the upload address of a copy, and that only gets an encrypted copy. Run two watchers, and one hacked watcher can't silence you.

## If it breaks

- **The watcher is down.** Your servers keep running and keep their backups. After five reports no watcher took, each daemon alerts: `no watcher is reachable`. With two watchers, the other one still alerts.
- **A server is silent.** The watcher alerts once. When the server reports again, it says it is back.
- **The watcher was off for a while.** That time does not count as silence. It starts counting again when it starts.
