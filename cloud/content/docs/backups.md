---
title: "Backups"
description: "SQLite, Postgres, MySQL, MariaDB, MongoDB, ClickHouse and Redis. Encrypted and copied to your bucket or moin cloud. Every day one copy is restored and read before it leaves."
group: "Servers and data"
order: 1
---

A backup you never restored is a hope. So every day moin restores one for real, and reads it, before it leaves the server.

## What moin backs up

Databases, not files.

- **SQLite**: every database file in a folder the app mounts, like `./data`.
- **Postgres**: every database in a service that runs a Postgres image. No password, no change to your `compose.yml`.
- **MySQL and MariaDB**: the database `MYSQL_DATABASE` or `MARIADB_DATABASE` names, in a service that runs the official `mysql` or `mariadb` image. No change to your `compose.yml`.
- **MongoDB**: every database in a service that runs the official `mongo` image. No change to your `compose.yml`.
- **ClickHouse**: every database in a service that runs the `clickhouse/clickhouse-server` or `clickhouse` image. No change to your `compose.yml`.
- **Redis and Valkey**: a service that runs the official `redis` or `valkey/valkey` image and mounts a volume at `/data`. That volume says it keeps data. Without it the Redis is a cache: it is empty after every deploy anyway, so moin does not back it up. `moin backups my-app` lists it as `cache, not backed up`.

moin finds them by itself, also a database your app makes long after the deploy.

## The bucket

The copies go into a bucket you own, at any S3-compatible provider: Cloudflare R2, AWS S3, Backblaze B2, Hetzner and others. Never through us.

```bash
moin bucket set my-server
```

moin asks for the endpoint, the bucket's name, the access key and the secret key. The secret never lands in your shell history. The server writes a test file into the bucket. Only when that works does it keep the bucket. Then it tries to delete the file again, to see whether your bucket is [locked](#lock-your-bucket):

```text
✓ my-server backs up to backups. The first copies come within a minute
✓ Locked: a hacked server can't delete your copies.
```

Right after, moin shows your backup key, once, and asks you to paste it back. [Your backup key](#your-backup-key)

- In a script, pass `--endpoint`, `--bucket` and `--access-key`, and the secret key on stdin:

  ```bash
  echo "$SECRET_KEY" | moin bucket set my-server --endpoint https://s3.example.com --bucket backups --access-key <access key>
  ```

- `--region` is the bucket's region, default `us-east-1`. `--path` is a folder in the bucket. Leave `--endpoint` out for AWS S3.
- The key lives on the server, in `/etc/moin/config.yml`, readable only by root. [Server config](/docs/config#bucket)
- `moin bucket my-server` shows what is set, never the secret. It checks the lock again, and `--json` says `"locked": true`.

Without a bucket and without [moin cloud](#copies-to-moin-cloud), moin copies nothing. `moin bucket my-server` says so: `my-server has no bucket yet, so nothing is backed up.` And `moin` shows a `!` line for each app with a database: `my-app: no backup yet.`

## Lock your bucket

A hacker who takes your server has its keys to the bucket. With them, they could delete your copies too. A lock stops that. A locked bucket keeps every new file for some days, and nobody can delete it before. Not the server, not a hacker, not you.

moin checks it for you. Without a lock, `moin bucket set` and `moin bucket my-server` say:

```text
! Not locked: a hacked server could delete your copies.
  Turn on a lock: https://moin.run/docs/backups#lock-your-bucket
```

That is a hint, not an error. Your backups work either way. `moin` shows it as a grey line: `my-server: the bucket is not locked.`

Lock for 7 days. That is how long moin keeps a copy. Keep copies longer in the [config](#how-often)? Lock for as long, not longer.

Turn the lock on at your provider, below. Then run `moin bucket my-server`. It checks again:

```text
✓ Locked: a hacked server can't delete your copies.
```

- The check leaves a small file in the bucket: `<path>/<server>/moin-lock-check`. Each check writes the same one. A bucket that refuses to overwrite it gets one more per check, with the time in its name. Each goes away when its lock ends.
- A lock means what it says. Nobody can delete a file before its days are up, also the provider's support. That is the point.
- Keys that may write but not delete are no lock. They can still write over a copy. So moin says Not locked.

### Cloudflare R2

1. Open the [Cloudflare dashboard](https://dash.cloudflare.com), then R2 Object Storage.
2. Click your bucket, then the Settings tab.
3. Find Bucket lock rules and click Add rule.
4. Name it `moin`. Leave the prefix empty. Retention: 7 days.
5. Click Save changes.

### AWS S3

A lock needs a new bucket.

1. Open the S3 console and click Create bucket.
2. Give it a name. Under Bucket Versioning, pick Enable.
3. Under Object Lock, pick Enable, and tick the box that you understand.
4. Click Create bucket.
5. Open the new bucket, then Properties. Under Object Lock, click Edit.
6. Default retention: Enable. Mode: Compliance. 7 days. Save changes.
7. Then Management, Create lifecycle rule. Apply it to all objects. Tick Permanently delete noncurrent versions of objects, after 1 day. Save. Without it, deleted copies stay hidden in the bucket and cost money.

Then `moin bucket set my-server` with the new bucket.

### Backblaze B2

1. Open Buckets and click Create a Bucket.
2. Give it a name. Files: Private. Object Lock: Enable.
3. Click Create a Bucket.
4. On the new bucket, click Object Lock. Default retention: Compliance, 7 days. Update.
5. Click Lifecycle Settings. Pick Keep prior versions for this number of days: 1. Update.

### MinIO

Make a new bucket with a lock, then give it 7 days, with MinIO's `mc` tool:

```bash
mc mb --with-lock local/backups
mc retention set --default compliance 7d local/backups
```

## Copies to moin cloud

No bucket of your own? With a paid [moin cloud](/docs/cloud#a-copy-your-server-cant-delete) workspace, the copies go there. With a bucket, they go to both.

```bash
moin watcher connect my-server
moin bucket key my-server
```

- **What goes**: every scheduled copy of every database, on the [schedule](#how-often).
- **Encrypted with your key**: on the server, for the server's key and [your backup key](#your-backup-key). moin cloud can't read a copy. A restore from moin cloud needs your key, so make it first: `moin bucket key my-server`.
- **Proven first**: the first copy of each day is restored and read on your server before it leaves. A copy that does not restore is not uploaded. [The daily check](#the-daily-check)
- **Out of reach**: your server can add a copy there. It can never read, list or delete one. Each stays 14 days.
- **Restore**: `moin backups my-app` lists them beside the copies in your bucket, WHERE says `moin cloud`. `moin restore my-app` puts one back, also on a new server. [Restore from moin cloud](/docs/cloud#a-copy-your-server-cant-delete)

Without a bucket there is no copy before a deploy. `moin backup`, `moin backups` and `moin restore` work on your bucket only.

## Encryption

Every copy is encrypted on the server before it leaves, with [age](https://age-encryption.org). Your bucket only ever holds encrypted copies.

Each copy opens with either of two keys:

- **The server's key.** The server makes it before its first copy, in `/etc/moin/backup.key`, root only. The daily check and a restore open the copies with it.
- **Your backup key.** It is made on your laptop and shown to you once. The server only gets its public half, `age1…`. With it, the server can write copies for you, but never read one.

When the server is gone, its key is gone with it. Then only your key opens its copies. So keep it in your password manager.

## Your backup key

`moin bucket set` makes it for a server that has none yet:

```text
Your backup key for my-server. It is shown once.
Every copy is encrypted before it leaves the server. When the server is gone,
only this key reads the copies. Keep it in your password manager.

  AGE-SECRET-KEY-1…

? Paste your backup key, to confirm that you have it:
✓ Your key opens the copies of my-server: it read the copy 20261127T140000Z-118204 of /data/my-app.db from the bucket to the end
```

To confirm it, moin takes a fresh copy of the server's smallest database. It fetches that copy as it lies in the bucket, and opens and reads it on your laptop, with the key you pasted. Only then does the server note that you have the key.

- Until you confirm it, `moin` shows a `!` line: `my-server: your backup key is not confirmed.` Confirm it later with `moin bucket confirm my-server`.
- `moin bucket my-server` says whether every copy is encrypted for a key you confirmed, and shows its public half.
- `moin bucket key my-server` makes a new key. The copies made so far only open with the old one, so it asks first.
- One key for several servers: `moin bucket key staging --public age1…` with the public half of the key you have. `moin bucket production` shows it.
- Your key goes to the server only for a restore with `--key`, on stdin, never on a command line. The server uses it for that restore and writes it nowhere.

## See your backups

```bash
moin backups my-app
```

Every database of the app, its copies, the last restore check, and which copy that check proved: it says `✓ checked`. WHERE says where a copy lies: `bucket` or `moin cloud`.

```text
/data/my-app.db
  ✓ Checked 3h ago: the copy marked checked restored and read fine
COPY                      WHERE    TAKEN    SIZE
20261127T140000Z-118204   bucket   7m ago   2.1 MB
20261127T130000Z-482913   bucket   1h ago   2.1 MB   ✓ checked
  Go back to one with moin restore my-app --to <copy>
```

A SQLite database is named by its path in the container. Every other database by its service, like `postgres-db`, `mysql-db`, `mongo-db`, `clickhouse-events` or `redis-queue`.

It lists the newest 10 copies. `--json` lists them all. A copy from before every copy was encrypted says `not encrypted` next to its size, until it expires.

## How often

A copy every hour, each kept for 7 days. The three newest copies always stay, and so does the copy the last check restored. `moin backups my-app` shows the schedule on its first line. Set another one for one app, or for the server:

```bash
moin backups my-app --every 15m --keep 30d
moin backups -s my-server --every 6h
```

- `--every` takes a copy that often: 5 minutes at least, like `15m`, `1h` or `1d`.
- `--keep` keeps each copy that long: 1 day at least, like `7d` or `30d`.
- Give one of them, and the other stays as it is.
- With `-s` and no app, it is the server's schedule. Every app without its own takes it.
- It counts within a minute. Nothing restarts.

Both land in the server's [config](/docs/config#backups), next to the comments you wrote there:

```yaml
backups:
  every: 1h
  keep: 7d
  apps:
    my-app:
      every: 15m
      keep: 30d
```

## How a copy is made

```text
SQLite:   VACUUM INTO a temp file ─▶ gzip ─▶ age ─▶ a file on the server
Postgres: pg_dump ─▶ age ─▶ a file on the server
MySQL:    mysqldump or mariadb-dump ─▶ gzip ─▶ age ─▶ a file on the server
MongoDB:  mongodump, gzipped ─▶ age ─▶ a file on the server
ClickHouse: BACKUP into a zip ─▶ age ─▶ a file on the server
Redis:    redis-cli --rdb ─▶ gzip ─▶ age ─▶ a file on the server
Then:     the first copy of the day is restored and read ─▶ your bucket, moin cloud
```

Each makes a clean copy while your app keeps running.

- **SQLite** needs room on the disk for one copy, for a moment. moin looks first.
- **Postgres** copies run inside your Postgres container, so no password leaves it. Every database in it gets its own copies.
- **MySQL and MariaDB** copies run inside your database container too, with the dump tool of your own image. One consistent state, with routines, triggers and events. moin logs in as root with the `MYSQL_ROOT_PASSWORD` or `MARIADB_ROOT_PASSWORD` of the service. It reads the password inside the container, so it never leaves it.
- **MongoDB** copies run inside your MongoDB container, with the `mongodump` of your own image. One copy holds every database of the service. moin logs in with the `MONGO_INITDB_ROOT_USERNAME` and `MONGO_INITDB_ROOT_PASSWORD` of the service, if it has them. The password never leaves the container. MongoDB dumps one collection after the other, so writes during a copy can land in one collection and miss another.
- **ClickHouse** copies are made by ClickHouse's own `BACKUP`. One copy holds every database of the service. ClickHouse writes the zip into the `backups` folder of its data first, so its disk needs room for one copy, for a moment. moin reads the zip out and removes it. The official image allows that folder as it comes. A config of your own that sets `backups` must keep `<allowed_path>backups</allowed_path>`. moin logs in with the `CLICKHOUSE_USER` and `CLICKHOUSE_PASSWORD` of the service, if it has them. Tables that only point somewhere else, like Kafka or MySQL tables, come back as tables without data. Replicated tables need their Keeper, so their check fails.
- **Redis and Valkey** copies are the dump Redis sends a replica, fetched with the `redis-cli` of your own image. It works with dumps, with an append only file and with both. The dump lies in `/data` for a moment, so that volume needs room for one copy. A Redis with a password: moin reads it from `--requirepass` in the `command` of the service. A password in a config file it cannot see, so set `REDISCLI_AUTH` in the `environment` of the service then.

A copy on the spot, before something risky:

```bash
moin backup my-app
```

```text
copying /data/my-app.db
✓ Copied /data/my-app.db of my-app: 20261127T140000Z-118204
```

Every deploy takes one too, just before the new release touches the data. If that copy fails, the deploy stops, so the way back is never missing. `moin deploy --no-copy` deploys all the same. A rollback goes on without it.

## The daily check

The first copy of each day is checked before it leaves the server. moin opens it with the server's key and restores it into a throwaway database on your server. It reads every page or every row. Then it drops it. Only then does the copy go up, the very bytes that were read.

For MySQL, MariaDB, MongoDB, ClickHouse and Redis the throwaway database runs in a container of its own, from the same image as yours, without a network. The check never touches your live database. moin removes the container when it is done.

So "backed up" means: copied, restored and read. The check needs room on the disk for one more copy, for a while.

A database copied less often than once a day: on the days between, moin restores its newest copy from your bucket. The copy before a deploy and one from `moin backup` go up right away, without the check.

If it fails:

- That copy is not uploaded.
- You get an alert: `<app>: the restore check of <database> failed`.
- `moin backups my-app` shows it with a ✗, and the last copy that did restore.
- `moin` shows it with a `!`: `my-app: today's backup is not proven. Last proven copy: Nov 26 14:00 UTC.`
- The next copy, an hour later, is checked before it leaves too, until one restores.

## Restore

```bash
moin restore my-app
```

moin lists the copies, and you pick one: `? Go back to which copy? [20261127T140000Z-118204]`. Enter takes the newest. In a script, name it: `--to` takes a copy from `moin backups my-app`, or a time in UTC like `--to 2026-11-27T14:07`. Then moin takes the newest copy at or before it. An app with several databases: moin asks which one, or `--database <name>` names it.

A restore never changes your data in place:

1. moin builds the database beside the live one. Your app keeps running.
2. It stops the app for one quick switch, and starts it again.
3. It keeps the database from before, as the undo.

MySQL, MariaDB, MongoDB, ClickHouse and Redis cannot switch a database in one step. So there moin stops the app, keeps the live database as the undo, a dump on the server, and loads the copy. If the load fails, the undo goes back in and your data is as it was.

A MongoDB restore replaces every database of the service but `admin` and `config`. So the users of your MongoDB stay as they are. A ClickHouse restore replaces every database of the service but ClickHouse's own. A Redis restore starts a second Redis with the copy next to yours and has yours take all its data, as a replica does. Your Redis keeps running and writes its own files.

A restore replaces live data, so moin asks first. Scripts pass `-y`.

```text
? Put the data of my-app back to 20261127T130000Z-482913? The app stops for a moment. [y/N] y
restoring /data/my-app.db to 20261127T130000Z-482913
✓ /data/my-app.db of my-app is back at 20261127T130000Z-482913
  Undo it with moin restore my-app --to undo-20261127T150000Z-382601
```

## Undo a restore

```bash
moin restore my-app
```

Every restore keeps the data from right before it. `moin backups my-app` lists it as `undo-…`, `before a restore`, with WHERE `server`. In a script it is `--to undo-…`. Going back to it is a restore too, so you can undo the undo.

An undo stays as long as the copies do: 7 days, unless the [config](/docs/config#backups) says otherwise.

## Look at your data

```bash
moin sql my-app --tables
moin sql my-app "SELECT id, email FROM users ORDER BY id DESC"
```

```text
id   email
42   ada@example.com
41   NULL
```

- It asks the database your app runs on: SQLite, Postgres, MySQL or MariaDB. Nothing to install, no port to open. moin reads a SQLite file itself. Postgres and MySQL are asked by the client in their own container.
- It only reads. A statement that would change something fails, and says so.
- 200 rows come back at most. `--limit 1000` takes more. A long value is cut in the table. `--json` prints all of it.
- An app with several databases: moin asks which one, or `--db <name>` names it, as `moin backups my-app` lists them.

To change data, add `--write`:

```bash
moin sql my-app "UPDATE users SET plan = 'pro' WHERE id = 42" --write
```

```text
  UPDATE users SET plan = 'pro' WHERE id = 42
? Run this on my-app? moin takes a copy of the database first. [y/N] y
copying postgres-db
running the statement
✓ 1 row changed in postgres-db of my-app
  Undo it with moin restore my-app --to before-sql
```

1. moin shows the statement and asks. Scripts pass `-y`.
2. It takes a copy of the database into your bucket, like `moin backup`.
3. It runs the statement in one transaction. If it fails, nothing changed. One exception: MySQL and MariaDB keep a `CREATE`, `ALTER` or `DROP` right away.
4. `moin restore my-app --to before-sql` goes back to the copy from before your last write. It is a restore: what the app wrote since goes too.

A server without a bucket has no place for the copy. Set one, or write without a copy: `--no-copy`.

## When the server is gone

Your copies are in your bucket, and your backup key opens them. Set up a new server, here `new`, and deploy the app there:

```bash
cd my-app
moin deploy new
moin bucket set new
moin backups my-app -s new --from my-server
moin restore my-app -s new --from my-server --key
```

- `moin bucket set new` takes the same bucket. Give it your old key with `moin bucket key new --public age1…`, or take a new one.
- `--from my-server` reads the copies the server `my-server` made, under its name: in the bucket, and in moin cloud.
- `--key` asks for your backup key. The new server's own key can't open the old copies.
- In a script, name the copy with `--to <copy>`, pipe the key in, and add `-y`.

With [moin cloud](/docs/cloud#a-copy-your-server-cant-delete), the same commands show and restore the copy your server could not delete. Its WHERE is `moin cloud`, and it always asks for your backup key.

## Download a copy

```bash
moin backup download my-app
```

The newest copy of the app's database comes to your laptop as a file you can open: `my-app-20261127T140000Z-118204.db`.

- The server sends the copy as it lies in the bucket, encrypted, over SSH. Your backup key opens it on your laptop. It never goes to the server, and no link is made.
- What you get: a SQLite file, a Postgres dump (`.dump`, for `pg_restore`), a gzipped MySQL dump (`.sql.gz`), or a gzipped MongoDB archive (`.archive.gz`).
- `--to <copy>` takes another copy from `moin backups`. `--database <name>` picks one when the app has several.
- `-o file` names the file. `-o -` writes it to stdout.
- moin asks for your backup key and does not show it. `--key-file key.txt` reads it from a file. A script pipes it in.
- A server without a bucket sends a fresh copy instead.

## Your copies without moin

The copies of a database are in `<path>/<server>/<app>/<database>/copies/` in your bucket. Each is an [age](https://age-encryption.org) file. Put your backup key in a file, `key.txt`, and open a copy with the `age` tool:

```bash
age -d -i key.txt 20261127T140000Z-118204.db.gz.age | gunzip > my-app.db
age -d -i key.txt 20261127T140000Z-118204.dump.age > my-app.dump
age -d -i key.txt 20261127T140000Z-118204.sql.gz.age | gunzip > my-app.sql
age -d -i key.txt 20261127T140000Z-118204.archive.gz.age > my-app.archive.gz
age -d -i key.txt 20261127T140000Z-118204.zip.age > my-app.zip
age -d -i key.txt 20261127T140000Z-118204.rdb.gz.age | gunzip > dump.rdb
```

A SQLite copy, `.db.gz.age`, is then a file for `sqlite3`. A Postgres copy, `.dump.age`, is a `pg_dump -Fc`: `pg_restore` puts it into any Postgres. A MySQL or MariaDB copy, `.sql.gz.age`, is plain SQL: `mysql` or `mariadb` loads it. A MongoDB copy, `.archive.gz.age`, is an archive of `mongodump`: `mongorestore --archive=my-app.archive.gz --gzip` loads it. A ClickHouse copy, `.zip.age`, is a zip of `BACKUP`: put it in the `backups` folder of a ClickHouse and run `RESTORE ALL FROM File('my-app.zip')`. A Redis copy, `.rdb.gz.age`, is a `dump.rdb`: a Redis without an append only file loads it from its data folder when it starts. You need no moin for any of them.

## If it breaks

- **The server dies during a restore.** The switch is one step, so the database is whole either way. When the daemon starts again, it finishes or rolls back, and tells you.
- **The disk is too full.** moin checks for room before a copy, a check or a restore, and says so instead of starting.
- **The server is gone.** Your copies are in your bucket, and your backup key opens them. [When the server is gone](#when-the-server-is-gone)
- **The backup key is lost, and the server is gone.** Then nobody can open the copies, not even us. Keep the key in your password manager.
