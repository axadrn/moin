---
title: "Server config"
description: "/etc/moin/config.yml, key by key: the server's name, its watchers, alerts, bucket, backup key, backup schedule, mounts and server access."
group: "Reference"
order: 2
---

Each server has one config file: `/etc/moin/config.yml`. It says what the server is called, where it reports, where alerts go, where the copies go, and how often.

You rarely write it by hand. The commands write it for you:

| Command | Writes |
| --- | --- |
| the first `moin deploy` | `name` |
| `moin watcher connect`, `moin watcher disconnect` | `watchers`: moin cloud is one |
| `moin alerts add`, `moin alerts rm` | `alerts` |
| `moin bucket set` | `bucket` |
| `moin bucket key`, `moin bucket confirm` | `backup_key` |

The backup schedule, `mounts` and `server_access` you set by hand: `ssh my-server`, then edit the file as root.

## Every key

```yaml
# /etc/moin/config.yml. Root only: it holds the bucket's keys.

# The server's name. Its copies live under it in the bucket, and
# watchers know the server by it. The first deploy writes your SSH
# alias here. Letters, digits, dots and dashes. Keep it: a new name
# means a new folder in the bucket.
name: my-server

# The watchers this server reports to, every minute.
# `moin watcher connect my-server <url> <token>` adds one.
watchers:
  - url: https://watcher.example.com
    token: <the token from moin watcher token my-server>

# Where the server's own alerts go.
# `moin alerts add` adds one, after a test alert went out.
alerts:
  - kind: telegram
    token: <bot token>
    chat_id: <chat id>
  - kind: slack          # slack, discord or webhook: each takes a url
    url: https://hooks.slack.com/services/<your hook>
  - kind: email          # through your SMTP server, with STARTTLS
    to: ada@example.com
    smtp: smtp.example.com:587
    user: alerts@example.com
    password: <the password of that user>

# Where the copies go: any S3-compatible bucket.
# `moin bucket set my-server` writes it, after a test file went in.
bucket:
  endpoint: https://s3.example.com   # leave it out for AWS S3
  region: auto                       # default us-east-1
  bucket: backups                    # needed
  path: moin                         # a folder in the bucket, optional
  access_key: <access key>           # needed
  secret_key: <secret key>           # needed
  locked: true                       # the server writes it at each check

# The public half of your backup key: every copy is encrypted for it.
# `moin bucket key my-server` writes it, `moin bucket confirm my-server` sets
# confirmed once your key opened a copy.
backup_key:
  public: age1…
  confirmed: true

# How often each database is copied, and how long a copy is kept.
backups:
  every: 1h        # default 1h, at least 5m
  keep: 7d         # default 7d, at least 1d
  apps:            # an app with its own schedule
    my-app:
      every: 15m
      keep: 30d

# Folders of the server an app may mount, besides its own.
mounts:
  - /mnt/media

# Allow compose settings that reach beyond the app, like a privileged
# container or the Docker socket. Off by default.
server_access: false
```

## The rules

- **Every key is optional.** A server without the file has no watcher, no alert and no bucket. Its name is then its hostname.
- **A key moin does not know is an error.** So a typo never goes unnoticed. The daemon alerts `the config has a mistake`, and the last good config keeps working until it is fixed. Commands say it too.
- **A change counts within a minute.** The daemon reads the file every minute. No restart.
- **A command keeps your comments.** When `moin bucket set` or another command changes the file, the comments you wrote stay.
- **Root only.** It holds the bucket's secret key, the alert tokens and passwords, and the watcher tokens. `moin bucket my-server` shows the bucket, never the secret.

## backups

A time is a number and a unit: `m` minutes, `h` hours, `d` days. Like `15m`, `1h`, `7d`.

- `every`: how often a copy is taken. Default `1h`. Less than `5m` is an error.
- `keep`: how long a copy stays in the bucket, counted from the newest copy. Default `7d`. Less than `1d` is an error. The three newest copies always stay, and so does the copy the last check restored.
- `apps`: a schedule for one app. What it leaves out comes from the server's.

The undo a restore leaves behind is kept just as long as the copies.

## bucket

- `endpoint`: the S3 address of your provider, like `https://<account>.r2.cloudflarestorage.com`. Leave it out for AWS S3.
- `region`: the bucket's region. Default `us-east-1`. Cloudflare R2 takes `auto`.
- `bucket`, `access_key`, `secret_key`: needed, all three.
- `path`: a folder in the bucket. Then copies go to `<path>/<server>/<app>/<database>/copies/`.
- `locked`: the bucket refused to delete a fresh file at the last check. The server writes it, at `moin bucket set` and `moin bucket my-server`. [Lock your bucket](/docs/backups#lock-your-bucket)

## backup_key

- `public`: the public half of your backup key, `age1…`. Every copy is encrypted for it, and for the server's own key in `/etc/moin/backup.key`.
- `confirmed`: you showed that you have the private half, by opening a copy with it. Until then `moin` shows a `!` line: `my-server: your backup key is not confirmed.`

The private half is never on the server. [Your backup key](/docs/backups#your-backup-key)

## mounts and server_access

An app may mount its own folders, like `./data`, and its own named volumes. Nothing else of the server. A deploy that mounts more is refused, and says what to allow.

- `mounts`: folders of the server an app may mount too, like `/mnt/media`.
- `server_access: true`: allows every compose setting, also those that reach beyond the app, like `privileged` or `/var/run/docker.sock`. Every app on the server gets it. Turn it on only when you trust every app on the server.

## Elsewhere on the server

| Path | What |
| --- | --- |
| `/etc/moin/config.yml` | this file. `MOIN_CONFIG` points moin at another one |
| `/etc/moin/backup.key` | the server's own backup key, next to this file. Root only |
| `/var/lib/moin/server.json` | what is closed, and the Cloudflare Tunnel token. Root only |
| `/var/lib/moin/` | the apps, their deploys and what the daemon knows |
| `/root/.docker/config.json` | the logins of private registries, from `moin registry set`. Not moin's file: it is Docker's |
| `/usr/local/bin/moin` | the program |
| `moin-daemon` | the systemd service of `moin daemon` |

The watcher has its own file, `/etc/moin/watcher.yml`. [Watcher](/docs/watcher#its-config)
