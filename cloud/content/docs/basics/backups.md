---
title: "What a backup is"
description: "A backup you never restored is a hope. The 3-2-1 idea in plain words. What moin does: a check every day. Your own key. A copy at moin cloud."
group: "Basics"
order: 9
---

A backup is a copy of your data, somewhere else. When the original breaks, the copy brings it back.

That last part is the whole point. A copy that does not come back is no backup.

## A backup you never restored is a hope

Copies fail quietly. The job ran, but wrote an empty file. The file is there, but cut off halfway. The password to open it is gone. The database changed, and the copy is of the wrong one.

You only find out on the day you need it. That is the worst day to find out.

So a backup is only proven when you have restored it, and read what came back.

## The 3-2-1 idea

An old rule, easy to remember:

```text
3 copies of your data
2 different kinds of places
1 of them far away
```

- **3 copies.** The live data, and two copies. One copy can break too.
- **2 kinds of places.** Not two folders on the same disk. A disk and a bucket, say.
- **1 far away.** Another building, another provider. A fire, a lost account or a hacked server takes one place, not all.

## What can go wrong

Each one needs a copy somewhere else:

- **You delete the wrong rows.** The copy from an hour ago saves you.
- **A bad deploy breaks the data.** The copy from just before it.
- **The disk dies, or the provider loses the server.** A copy that is not on that server.
- **Someone breaks into the server.** A copy they can't delete from there.

The provider's snapshots help with some of these. They live at the same provider, often in the same place, and you can rarely open one to check it.

## What moin does

moin finds your SQLite, Postgres, MySQL, MariaDB, MongoDB and ClickHouse databases by itself. Once you give it a bucket, it copies each one every hour.

```bash
moin bucket set my-server
```

A bucket is storage for files, at any S3-compatible provider. Pick one at another provider than your server. That is the "far away".

Then, every day:

1. moin takes the newest copy.
2. It restores it into a throwaway database, on your server.
3. It reads every page or every row.
4. It throws it away again.

So "backed up" means: copied, restored and read. If that fails, you get an alert, and `moin` shows a `!` line.

```bash
moin backups my-app
```

```text
/data/my-app.db
  ✓ Checked 3h ago: the copy marked checked restored and read fine
```

## Your key

Every copy is encrypted on the server, before it leaves. Your bucket only ever holds locked boxes.

Two keys open them:

- **The server's key.** For the daily check.
- **Your backup key.** moin shows it to you once, when you set the bucket. Put it in your password manager.

When the server is gone, its key is gone with it. Then only your key opens the copies. So moin makes you paste it back once, and opens a real copy with it, on your laptop. Now you know it works.

## A copy the server can't delete

A server that is broken into holds the key to its bucket. That key can delete copies too.

[moin cloud](/docs/cloud#a-copy-your-server-cant-delete) keeps one more copy, where your server can only add, never delete. It is encrypted for your key, so we can't read it. That is the third copy, in a third place.

## Restore

```bash
moin restore my-app
```

Pick a copy, and moin puts it back. It builds the database beside the live one, switches in one quick step, and keeps the old data as an undo. [Restore](/docs/backups#restore)
