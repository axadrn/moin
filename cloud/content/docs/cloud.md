---
title: "moin cloud"
description: "A watcher we run for you. 30 days free from the first report, then $7 a month or $49 a year. It can't command your server."
group: "Servers and data"
order: 4
---

moin cloud is a [watcher](/docs/watcher) we run for you, on our servers. Your apps stay on your server. Your server reports to moin cloud every minute. When the reports stop, or a domain is down from outside, you get a mail.

## Start

Sign in on your laptop, then connect a server:

```bash
moin cloud login
moin cloud connect prod
```

`moin cloud login` opens the browser. Sign in with GitHub or a link we send to your mail. No password. The page shows a code; allow it when it is the one in your terminal.

`moin cloud connect prod` gives prod a token. From then on prod reports to moin cloud. Connect every server the same way.

```bash
moin cloud                     # your plan, billing, your servers, what needs you
moin cloud disconnect prod     # prod no longer reports
```

The same is on [moin.run/cloud](/cloud) in the browser. It only shows. Nothing on it changes a server.

## Your account

Your mail is your account. One mail, one workspace, every server in it. GitHub and the mail link land in the same workspace when the mail is the same.

Delete it under Settings. That cancels your subscription first, and you get a mail. Then the workspace, the tokens of its servers and every session are gone. Your servers keep running.

## Trial

The first report of your first server starts 30 days free. No card. You get a mail five days before the end, and one at the end.

After the trial your servers still report, and the page still shows them. But no alert goes out until you subscribe.

## Price

$7 a month, or $49 a year, for the whole workspace. Every server, every app.

- Subscribe on [moin.run/cloud](/cloud). The checkout is Stripe's.
- For businesses only.
- Stripe sells it to you, as the seller of record, and handles the tax. The invoice comes from Stripe.
- `moin cloud` and the page link Billing: your invoices, your card, cancel.
- Cancel there any time. Alerts stay on until the paid time ends. Then you get a mail, and alerts are off.

Early backers of the hosted hub sign in with the same mail. Their discount is applied at the checkout by itself.

## What it can't do

moin cloud receives reports. It sends nothing back that your server acts on.

- It can't deploy, restart or stop anything.
- It can't log in to your server. It has no key.
- If moin cloud is down, nothing changes on your server. Your apps run, your backups run.

It sees what a report holds: names of servers and apps, domains, versions, times. Never your data, never your env.

## Coming: a copy your server can't delete

Your server holds the key to your own bucket. That key can delete copies, because old copies have to go. So someone who takes over your server can delete your backups too.

moin cloud will keep a second copy, for paying workspaces:

- Your server can add copies. It can never delete one.
- Each copy stays 14 days, then goes.
- Encrypted on your server, with your key. moin cloud can't read it.
- It does not replace your bucket. It is a second copy.

## Another moin cloud

moin cloud is open source, in `cloud/` of the repo. To point the CLI at another one, like one on your laptop, set `MOIN_CLOUD_URL`:

```bash
MOIN_CLOUD_URL=http://localhost:8090 moin cloud login
```

Without it the CLI talks to https://moin.run.
