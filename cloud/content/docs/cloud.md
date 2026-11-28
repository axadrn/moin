---
title: "moin cloud"
description: "A watcher we run for you, and a copy of your backups your server can't delete. 30 days free, then $7 a month or $49 a year."
group: "Servers and data"
order: 4
---

moin cloud is a [watcher](/docs/watcher) we run for you, on our servers. Your apps stay on your server. Your server reports to moin cloud every minute. When the reports stop, or a domain is down from outside, you get a mail.

## Start

Connect a server, on your laptop:

```bash
moin watcher connect prod
```

The first time, it opens the browser. Sign in with GitHub or a link we send to your mail. No password. The page shows a code. Allow it when it is the one in your terminal.

```text
→ Sign in to moin cloud in the browser, and allow the code K7QF-92MD: https://moin.run/cli?code=K7QF-92MD
✓ Signed in to moin cloud
✓ prod reports to moin cloud every minute
  See it with `moin watcher`
```

Then prod gets a token. From then on prod reports to moin cloud. Connect every server the same way.

Alerts are set once, on the server: `moin alerts add prod telegram`. Each report carries them, so moin cloud alerts there too, and sends a mail. [Watcher](/docs/watcher#alerts)

```bash
moin watcher                       # each server's watchers, your plan, trial days left, billing
moin watcher disconnect prod       # prod no longer reports
```

The same is on [moin.run/cloud](/cloud) in the browser. It only shows. Nothing on it changes a server.

## Your account

Your mail is your account. One mail, one workspace, every server in it. GitHub and the mail link land in the same workspace when the mail is the same.

Delete it under Settings, on [moin.run/cloud](/cloud): type your mail to confirm. That cancels your subscription first. Then the workspace, the tokens of its servers and every session are gone. Its copies go as soon as their 14-day lock lets them. Your servers keep running, and your own bucket keeps its copies.

## Trial

The first report of your first server starts 30 days free. No card. You get a mail five days before the end, and one at the end.

After the trial your servers still report, and the page still shows them. But no alert goes out until you subscribe.

## Price

$7 a month, or $49 a year, for the whole workspace. Every server, every app.

- Subscribe on [moin.run/cloud](/cloud). The checkout is Stripe's.
- For businesses only.
- Stripe sells it to you, as the seller of record, and handles the tax. The invoice comes from Stripe.
- `moin watcher` and the page link Billing: your invoices, your card, cancel.
- Cancel there any time. Alerts stay on until the paid time ends. Then you get a mail, and alerts are off.

Early backers sign in with the mail they paid with. Their discount is applied at the checkout by itself.

## What it can't do

moin cloud receives reports and copies. It sends nothing back that your server acts on, but one address to upload a copy to. Your server goes there only over https, to a public address.

- It can't deploy, restart or stop anything.
- It can't log in to your server. It has no key.
- If moin cloud is down, nothing changes on your server. Your apps run, your backups run.

It sees what a report holds: names of servers and apps, domains, versions, times. And the size of each copy. Never your data, never your env.

## A copy your server can't delete

Your server holds the key to your own bucket. That key can delete copies, because old copies have to go. So someone who takes over your server can delete your backups too.

So moin cloud keeps a copy, for paying workspaces and during the trial:

- Each scheduled copy of every database goes to moin cloud. With your own bucket it goes there too, and moin cloud holds the second copy. Without one, moin cloud holds the only copy. [Copies to moin cloud](/docs/backups#copies-to-moin-cloud)
- Encrypted on your server, for the server's key and [your backup key](/docs/backups#your-backup-key). moin cloud can't read it. Make your key with `moin bucket key prod`: a restore needs it.
- Proven before it leaves: the first copy of each day is restored and read on your server. A copy that does not restore is not uploaded.
- Your server can add copies. It can never read, list, delete or overwrite one.
- Each copy stays 14 days, then goes. The newest copy of a database always stays.

```bash
moin backups shop               # the copies of shop, in your bucket and in moin cloud
moin restore shop               # put shop back to one
```

`moin backups shop` shows every copy in one table. WHERE says where it is: `bucket` or `moin cloud`. `moin restore shop` asks which one. Enter takes the newest. A copy in moin cloud asks for your backup key. In a script, name the copy with `--to <copy>` and pass `-y`.

It works for a server that is gone, too. Set up a new one, deploy shop there, then `moin restore shop -s new --from prod`. Like every restore, it keeps the data from before as an undo: `moin restore shop --to undo-…`.

### How it works

1. Your server asks moin cloud for one address to upload one copy to. The address works for 15 minutes, for exactly that many bytes, and only where nothing lies yet.
2. Your server uploads the copy there, and says it is done. moin cloud checks the copy is there.
3. For a restore, your laptop gets an address to download one copy, for 5 minutes. It hands that address and your key to the server over SSH. The server downloads and opens it. The data never passes your laptop or moin cloud's servers.

The copies lie in a Cloudflare R2 bucket with a 14-day lock. Inside it nobody deletes or overwrites a copy, not even moin cloud. Whoever owns that Cloudflare account could change the lock. That is us.

### Limits

- 5 GB of copies per workspace during the trial. 50 GB with a subscription or the free year, fair use. When it is full, no new copy goes in, and you get a mail. No old copy is deleted for it.
- One copy an hour per database.
- When a subscription ends, no new copy goes in. The copies stay 30 days, then they go.

## Another moin cloud

moin cloud is open source, in `cloud/` of the repo. To point the CLI at another one, like one on your laptop, set `MOIN_CLOUD_URL`:

```bash
MOIN_CLOUD_URL=http://localhost:8090 moin watcher connect prod
```

Without it the CLI talks to https://moin.run.
