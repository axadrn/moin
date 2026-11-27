---
title: "Backups"
description: "SQLite and Postgres, copied on a schedule into your bucket. Every day one copy is restored and read. Restore, and undo a restore."
group: "Servers and data"
order: 1
---

A backup you never restored is a hope. So every day moin restores one for real, and reads it.

## What moin backs up

Databases, not files.

- **SQLite**: every database file in a folder the app mounts, like `./data`.
- **Postgres**: every database in a service that runs a Postgres image. No password, no change to your `compose.yml`.

moin finds them by itself, also a database your app makes long after the deploy. MySQL and MongoDB run, but moin can't back them up yet. `moin backups shop` says so.

## The bucket

The copies go into a bucket you own, at any S3-compatible provider: Cloudflare R2, AWS S3, Backblaze B2, Hetzner and others. Never through us.

```bash
moin bucket set prod
```

moin asks for the endpoint, the bucket's name, the access key and the secret key. The secret never lands in your shell history. The server writes a test file into the bucket and deletes it again. Only when that works does it keep the bucket.

```text
✓ prod backs up to backups. The first copies come within a minute
```

- In a script, pass `--endpoint`, `--bucket` and `--access-key`, and the secret key on stdin:

  ```bash
  echo "$SECRET_KEY" | moin bucket set prod --endpoint https://s3.example.com --bucket backups --access-key <access key>
  ```

- `--region` is the bucket's region, default `us-east-1`. `--path` is a folder in the bucket. Leave `--endpoint` out for AWS S3.
- The key lives on the server, in `/etc/moin/config.yml`, readable only by root. [Server config](/docs/config#bucket)
- `moin bucket prod` shows what is set, never the secret.

Until there is a bucket, moin copies nothing. `moin bucket prod` says so: `prod has no bucket yet, so nothing is backed up.`

## See your backups

```bash
moin backups shop
```

Every database of the app, its copies, the last restore check, and which copy that check proved.

```text
/data/shop.db
  ✓ Restore check 3h ago: the copy 20261127T130000Z-482913 restored and read fine
COPY                      TAKEN    SIZE
20261127T140000Z-118204   7m ago   2.1 MB
20261127T130000Z-482913   1h ago   2.1 MB
  Go back to one with moin restore shop --to <copy>
```

A SQLite database is named by its path in the container. A Postgres database by its service, like `postgres-db`.

It lists the newest 10 copies. `--json` lists them all.

## How often

A copy every hour, each kept for 7 days. The newest copy always stays. Set another schedule for the server, or for one app, in its [config](/docs/config#backups):

```yaml
backups:
  every: 1h
  keep: 7d
  apps:
    shop:
      every: 15m
      keep: 30d
```

## How a copy is made

```text
SQLite:   VACUUM INTO a temp file ─▶ gzip ─▶ your bucket
Postgres: pg_dump ─▶ straight into your bucket
```

Both make a clean copy while your app keeps running.

- **SQLite** needs room on the disk for one copy, for a moment. moin looks first.
- **Postgres** copies run inside your Postgres container, so no password leaves it. Every database in it gets its own copies.

A copy on the spot, before something risky:

```bash
moin backup shop
```

## The daily check

Every day moin takes the newest copy and restores it into a throwaway database on your server. It reads every page or every row. Then it drops it.

So "backed up" means: copied, restored and read. The check needs room on the disk for one more copy, for a while. If it fails, you get an alert, `<app>: a backup of <database> did not restore`, and `moin backups shop` shows it with a ✗.

## Restore

```bash
moin restore shop
```

moin lists the copies, and you pick one. Enter takes the newest. In a script, name it: `--to` takes a copy from `moin backups shop`, or a time in UTC like `--to 2026-11-27T14:07`. Then moin takes the newest copy at or before it. An app with several databases: moin asks which one, or `--database <name>` names it.

A restore never changes your data in place:

1. moin builds the database beside the live one. Your app keeps running.
2. It stops the app for one quick switch, and starts it again.
3. It keeps the database from before, as the undo.

A restore replaces live data, so moin asks first. Scripts pass `-y`.

```text
? Put the data of shop back to 20261127T130000Z-482913? The app stops for a moment. [y/N] y
✓ /data/shop.db of shop is back at 20261127T130000Z-482913
  Undo it with moin restore shop --to undo-…
```

## Undo a restore

```bash
moin restore shop
```

Every restore keeps the data from right before it. `moin backups shop` lists it as `undo-…`, `before a restore`. In a script it is `--to undo-…`. Going back to it is a restore too, so you can undo the undo.

An undo stays as long as the copies do: 7 days, unless the [config](/docs/config#backups) says otherwise.

## Your copies without moin

The copies of a database are in `<path>/<server>/<app>/<database>/copies/` in your bucket. A SQLite copy is gzipped: `gunzip` it and open it with `sqlite3`. A Postgres copy is a `pg_dump -Fc`: `pg_restore` puts it into any Postgres. You need no moin for either.

## Encryption

Coming. Each copy will be encrypted on the server before it leaves.

## If it breaks

- **The server dies during a restore.** The switch is one step, so the database is whole either way. When the daemon starts again, it finishes or rolls back, and tells you.
- **The disk is too full.** moin checks for room before a copy, a check or a restore, and says so instead of starting.
- **The server is gone.** Your copies are in your bucket. Set up a new server, deploy, and restore.
