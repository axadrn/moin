---
title: "A backup you never restored is not a backup"
description: "Back up Postgres and SQLite in Docker the safe way. Then prove the copy restores. The commands. How to run the check every day."
date: "2026-11-25"
---

Most backups fail the day you need them. Not because the copy was missing. Because nobody ever tried to restore it.

The file was empty. The dump stopped halfway. The SQLite file was copied while the app wrote to it. You find out at 3am, with the real database gone.

A backup counts once it has been restored. Here is how to do both, by hand.

## Postgres

### Make the copy

```bash
docker compose exec -T db pg_dump -U app -Fc app > app.dump
```

`-Fc` is Postgres' own compact format. `pg_dump` takes a consistent snapshot, so the app can keep writing while it runs.

### Prove it restores

Start a throwaway Postgres, restore into it, look inside:

```bash
docker run -d --name restore-test -e POSTGRES_PASSWORD=test postgres:17
until docker exec restore-test pg_isready -U postgres; do sleep 1; done

docker exec -i restore-test pg_restore -U postgres -d postgres --create < app.dump
docker exec restore-test psql -U postgres -d app -c "select count(*) from users"

docker rm -f restore-test
```

The count should match what your app has. If `pg_restore` fails, your backup was never a backup.

Use the same major version of Postgres as the app. A dump from 17 does not restore into 16.

## SQLite

### Make the copy

Never `cp` a SQLite file while the app runs. If it writes at that moment, the copy is broken, and with WAL mode the newest data sits in a second file you might miss.

SQLite has its own safe way:

```bash
sqlite3 data/app.db ".backup data/app-copy.db"
```

`.backup` copies the database page by page, consistent, while the app keeps working.

### Prove it restores

```bash
sqlite3 data/app-copy.db "PRAGMA integrity_check"
sqlite3 data/app-copy.db "select count(*) from users"
```

`integrity_check` answers `ok` for a good file. Then count something you know.

## Get it off the server

A copy on the same disk dies with the disk. Send it somewhere else: an S3 bucket, Cloudflare R2, Backblaze B2. Any S3-compatible bucket works with tools like `rclone`:

```bash
rclone copy app.dump r2:backups/$(date +%F)/
```

## Every day. Not once.

A restore test from March says nothing about today. Schemas change, disks fill up, credentials expire. Put the copy and the check in a cron job, and send yourself a message when the check fails. Not when it works: you stop reading those.

## How moin does it

[moin](/) does all of this for every SQLite and Postgres database of your apps. It copies them into your own bucket on a schedule. Every day it restores the newest copy into a throwaway database and reads it. When a check fails, it tells you.

[Backups in the docs](/docs/backups) has the details.
