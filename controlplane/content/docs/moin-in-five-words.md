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

The **agent** is the little helper moin puts on it. It only calls out to moin, like a child phoning home; it never opens the door to anyone.

## Deploy

A deploy puts a new version of your app live: v1, then v2, then v3.

Like hanging a new sign in the shop window. If the new sign falls down, the old one goes back up.

## Backup

moin writes down every change of your database for 7 days, so you can go back to any moment.

Like a diary of your data: turn back the pages to any day.

## Bucket

Your bucket is your own box on the internet, where your backups and uploads go. It is yours, not ours.

Like a locker you rent yourself: moin puts things in, but only you hold the key.

## How they connect

```text
you
 │ moin deploy
 ▼
moin (control plane)
 ▲
 │ the agent calls moin,
 │ nobody calls in
┌┴─────────────┐
│ server       │
│   agent      │
│   app shop ◀─┼── visitors
└┬─────────────┘
 │ every change
 ▼
your bucket
```

You deploy. The agent on your server picks it up and starts your app. Every change of its data goes into your bucket.

## Next

- [Get started](/docs/getting-started): your first app, in four steps.
- [How moin works](/docs/concepts/how-moin-works): the same words, with more detail.
