---
title: "What is a server"
description: "A server is a computer that never sleeps. A VPS is a slice of a big one that you rent. What to pick. What it costs."
group: "Basics"
order: 1
---

A server is a computer. It sits in a data center, not on your desk. It is always on. It has an address on the internet, so anyone can reach it.

Your app runs there. Your laptop can sleep. The app keeps running.

## A VPS

A data center has big computers. A provider cuts one into slices. Each slice acts like a computer of its own, with its own disk, memory and address. That slice is a VPS, a virtual private server.

```text
one big computer in a data center
┌─────────────────────────────────────┐
│ ┌────────┐ ┌────────┐ ┌────────┐    │
│ │ VPS    │ │ VPS    │ │ VPS    │    │
│ │ yours  │ │ someone│ │ someone│    │
│ │ 2 vCPU │ │ else's │ │ else's │    │
│ │ 4 GB   │ │        │ │        │    │
│ └────────┘ └────────┘ └────────┘    │
└─────────────────────────────────────┘
```

You rent it by the month, or by the hour. You get the key to it: you are root, the one user who may do everything. Nobody else logs in. Nobody else installs anything.

That is the difference to a platform that runs your app for you. On a VPS, the computer is yours. So are the choices, and the care.

## Where to rent one

Many providers rent them. Hetzner and DigitalOcean are two well-known ones. Vultr, Akamai, OVHcloud and Scaleway rent them too. They all give you the same thing: a Linux computer with an address.

moin works on any of them. It needs a 64-bit Linux server, Intel or ARM, that you can reach with SSH.

## What to pick

You click through a form at the provider. Pick this:

- **Image: Ubuntu LTS**, the newest one. LTS means long-term support: five years of security fixes. Its version is an even year and `.04`, like 24.04.
- **Size: 2 vCPU and 4 GB of memory.** A vCPU is a share of a processor. Your server builds your app itself, and a build needs memory. Smaller works for a tiny app. Bigger is one click later.
- **Disk: 40 GB or more.** Docker images, logs and your data live there.
- **Location: close to your visitors.** Every kilometer adds a little wait.
- **An IPv4 address.** Some providers make it an extra. Keep it: the instant address of your app needs one, and so do many visitors.
- **Your SSH key.** The form asks for it. Paste your public key there, so you log in with it, not with a password. [SSH and keys](/docs/basics/ssh)

Shared vCPU is fine to start. Dedicated vCPU costs more and is steadier under load.

## What it costs

A server of that size costs from about 5 to about 25 a month, in dollars or euros. It depends on the provider and the location. Prices change, so look at the provider's page.

Some extras cost a little more:

- an IPv4 address, at some providers
- the provider's own backups or snapshots of the whole disk
- traffic above what the plan includes, which is rare for a small app

The provider's snapshots are a copy of the whole disk. They are nice to have. They do not replace a backup of your database that you have restored once. [What a backup is](/docs/basics/backups)

## What you get

After a minute, the provider shows you an IP address, like `203.0.113.7`. That is your server's address on the internet.

Two things come with it:

- **The web console.** A screen and a keyboard for your server, in the provider's website. It works even when nothing else does. Remember where it is.
- **Root access over SSH.** From your laptop: `ssh root@203.0.113.7`.

The server is empty. Linux, and nothing else. moin installs the rest the first time you deploy.
