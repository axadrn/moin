---
title: "moin in five words"
description: "App, server, deploy, backup, bucket: the five words of moin"
group: "Start"
order: 1
---

All of moin is five words. Know them, and you know moin.

## App

Your app is the thing you made, like a shop or a blog, that people open on the internet.

Like a little shop: it has a door, and visitors come in.

## Server

A server is your computer on the internet. It is always on, so your app is always open.

The **daemon** is the little helper moin puts on it. It only calls out to moin, like a child phoning home; it never opens the door to anyone.

## Deploy

A deploy puts a new version of your app live: v1, then v2, then v3.

Like hanging a new sign in the shop window. If the new sign falls down, the old one goes back up.

## Backup

moin copies your database into your bucket on the schedule you pick, and keeps the copies as long as you say. With SQLite it can also write down every change, so you go back to any minute.

Like a diary of your data: turn back the pages to any day.

## Bucket

Your bucket is your own box on the internet, where your backups and uploads go. It is yours, not ours.

Like a locker you rent yourself: moin puts things in, but only you hold the key.

## How they connect

```text
you
 │ moin deploy
 ▼
moin (hub)
 ▲
 │ the daemon calls moin,
 │ nobody calls in
┌┴───────────────┐
│ server         │
│   daemon       │
│   app my-app ◀─┼── visitors
└┬───────────────┘
 │ copies
 ▼
your bucket
```

You deploy. The daemon on your server picks it up and starts your app. Copies of its data go into your bucket.

## Next

- [Get started](/docs/getting-started): your first app, in four steps.
- [How moin works](/docs/concepts/how-moin-works): the same words, with more detail.
