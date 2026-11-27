---
title: "Backups"
description: "SQLite and Postgres, encrypted and copied into your bucket on a schedule. Every day one copy is restored and read. Restore it, even with the server gone."
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

Right after, moin shows your backup key, once, and asks you to paste it back. [Your backup key](#your-backup-key)

- In a script, pass `--endpoint`, `--bucket` and `--access-key`, and the secret key on stdin:

  ```bash
  echo "$SECRET_KEY" | moin bucket set prod --endpoint https://s3.example.com --bucket backups --access-key <access key>
  ```

- `--region` is the bucket's region, default `us-east-1`. `--path` is a folder in the bucket. Leave `--endpoint` out for AWS S3.
- The key lives on the server, in `/etc/moin/config.yml`, readable only by root. [Server config](/docs/config#bucket)
- `moin bucket prod` shows what is set, never the secret.

Until there is a bucket, moin copies nothing. `moin bucket prod` says so: `prod has no bucket yet, so nothing is backed up.` And `moin` shows each app with a database in red: `shop: no backup yet.`

## Encryption

Every copy is encrypted on the server before it leaves, with [age](https://age-encryption.org). Your bucket only ever holds encrypted copies.

Each copy opens with either of two keys:

- **The server's key.** The server makes it before its first copy, in `/etc/moin/backup.key`, root only. The daily check and a restore open the copies with it.
- **Your backup key.** It is made on your laptop and shown to you once. The server only gets its public half, `age1…`. With it, the server can write copies for you, but never read one.

When the server is gone, its key is gone with it. Then only your key opens its copies. So keep it in your password manager.

## Your backup key

`moin bucket set` makes it for a server that has none yet:

```text
Your backup key for prod. It is shown once.
Every copy is encrypted before it leaves the server. When the server is gone,
only this key reads the copies. Keep it in your password manager.

  AGE-SECRET-KEY-1…

? Paste your backup key, to confirm that you have it:
✓ Your key opens the copies of prod: it read the copy 20261127T140000Z-118204 of /data/shop.db from the bucket to the end
```

To confirm it, moin takes a fresh copy of the server's smallest database. It fetches that copy as it lies in the bucket, and opens and reads it on your laptop, with the key you pasted. Only then does the server note that you have the key.

- Until you confirm it, `moin` shows the server in red: `prod: your backup key is not confirmed.` Confirm it later with `moin bucket confirm prod`.
- `moin bucket prod` says whether every copy is encrypted for a key you confirmed, and shows its public half.
- `moin bucket key prod` makes a new key. The copies made so far only open with the old one, so it asks first.
- One key for several servers: `moin bucket key web-1 --public age1…` with the public half of the key you have. `moin bucket prod` shows it.
- Your key goes to the server only for a restore with `--key`, on stdin, never on a command line. The server uses it for that restore and writes it nowhere.

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

It lists the newest 10 copies. `--json` lists them all. A copy from before every copy was encrypted says `not encrypted` next to its size, until it expires.

## How often

A copy every hour, each kept for 7 days. The three newest copies always stay, and so does the copy the last check restored. Set another schedule for the server, or for one app, in its [config](/docs/config#backups):

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
SQLite:   VACUUM INTO a temp file ─▶ gzip ─▶ age ─▶ your bucket
Postgres: pg_dump ─▶ age ─▶ straight into your bucket
```

Both make a clean copy while your app keeps running.

- **SQLite** needs room on the disk for one copy, for a moment. moin looks first.
- **Postgres** copies run inside your Postgres container, so no password leaves it. Every database in it gets its own copies.

A copy on the spot, before something risky:

```bash
moin backup shop
```

Every deploy takes one too, just before the new release touches the data. If that copy fails, the deploy stops, so the way back is never missing. `moin deploy --no-copy` deploys all the same. A rollback goes on without it.

## The daily check

Every day moin takes the newest copy, opens it with the server's key, and restores it into a throwaway database on your server. It reads every page or every row. Then it drops it.

So "backed up" means: copied, restored and read. The check needs room on the disk for one more copy, for a while.

If it fails:

- You get an alert: `<app>: a backup of <database> did not restore`.
- `moin backups shop` shows it with a ✗, and the last copy that did restore.
- `moin` shows it in red: `shop: today's backup did not restore. Last proven copy: Nov 26 14:00 UTC.`
- The check runs again an hour later, not a day.

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

## When the server is gone

Your copies are in your bucket, and your backup key opens them. Set up a new server, here `new`, and deploy the app there:

```bash
cd shop
moin deploy new
moin bucket set new
moin backups shop -s new --from prod
moin restore shop -s new --from prod --key
```

- `moin bucket set new` takes the same bucket. Give it your old key with `moin bucket key new --public age1…`, or take a new one.
- `--from prod` reads the copies the server `prod` left in the bucket, under its name.
- `--key` asks for your backup key. The new server's own key can't open the old copies.
- In a script, name the copy with `--to <copy>`, pipe the key in, and add `-y`.

With [moin cloud](/docs/cloud#a-copy-your-server-cant-delete), `moin cloud restore shop -s new` works the same, from the copy your server could not delete.

## Your copies without moin

The copies of a database are in `<path>/<server>/<app>/<database>/copies/` in your bucket. Each is an [age](https://age-encryption.org) file. Put your backup key in a file, `key.txt`, and open a copy with the `age` tool:

```bash
age -d -i key.txt 20261127T140000Z-118204.db.gz.age | gunzip > shop.db
age -d -i key.txt 20261127T140000Z-118204.dump.age > shop.dump
```

A SQLite copy, `.db.gz.age`, is then a file for `sqlite3`. A Postgres copy, `.dump.age`, is a `pg_dump -Fc`: `pg_restore` puts it into any Postgres. You need no moin for either.

## If it breaks

- **The server dies during a restore.** The switch is one step, so the database is whole either way. When the daemon starts again, it finishes or rolls back, and tells you.
- **The disk is too full.** moin checks for room before a copy, a check or a restore, and says so instead of starting.
- **The server is gone.** Your copies are in your bucket, and your backup key opens them. [When the server is gone](#when-the-server-is-gone)
- **The backup key is lost, and the server is gone.** Then nobody can open the copies, not even us. Keep the key in your password manager.
