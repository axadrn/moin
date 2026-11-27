---
title: "Tailscale"
description: "A private network of your own devices. Why SSH should only come over it, Tailnet Lock, the one rule a server needs, and key expiry."
group: "Basics"
order: 5
---

Tailscale builds a private network of your own devices. Your laptop, your phone, your servers. That network is your tailnet.

Inside it, your devices see each other. Outside it, nobody sees them.

```text
the internet: sees nothing on port 22
       ✗
┌──────────────── your tailnet ────────────────┐
│                                              │
│  laptop ◀──── encrypted, direct ────▶ prod   │
│  100.64.0.2                       100.64.0.5 │
│                                              │
└──────────────────────────────────────────────┘
```

Each device gets an address of its own, like `100.64.0.5`, and a name, like `prod.tail0000.ts.net`. The line between two devices is encrypted, with WireGuard. It works through home routers and office networks. The server needs no open port for it.

## Why SSH only over it

SSH on public port 22 is the door every bot knocks on. Over Tailscale, that door is gone for the internet. Only your devices can reach it.

So moin moves SSH there. Right after the first deploy, it asks:

```text
? Close public SSH, only Tailscale from now on? [Y/n]
```

Enter means yes. moin puts the server in your tailnet. It opens a new SSH session over Tailscale. Only when that works does port 22 close to the internet. Then it prints the new line for `~/.ssh/config`:

```text
  Point prod at the tailnet. In ~/.ssh/config, set:
    Host prod
      HostName prod.tail0000.ts.net
```

moin never edits that file itself. You change `HostName`, and `ssh prod` goes over Tailscale from then on. [SSH only over Tailscale](/docs/security#ssh-only-over-tailscale)

## What you need

- A Tailscale account. The free plan works.
- Tailscale on your laptop, logged in: [tailscale.com/download](https://tailscale.com/download).

moin installs Tailscale on the server. It asks for an auth key, or prints a link to log the server in. Make a key in the Tailscale admin console: Settings, Keys, Generate auth key.

## Tailnet Lock

Who controls your Tailscale account could add a device to your tailnet. That device could then reach your server's SSH.

[Tailnet Lock](https://tailscale.com/kb/1226/tailnet-lock) stops that. A new device joins only when one of your own devices signs it. Not even Tailscale can add one. Turn it on. It takes a few minutes, once.

## Tags and the one rule

A tailnet has rules. They say which device may reach which. The default lets your own devices reach each other, so a fresh tailnet needs nothing.

Once you change the rules, a server needs exactly one: your laptop may reach it on port 22. Give servers a tag, and allow that tag. In the admin console, Access controls:

```json
"tagOwners": {"tag:server": ["autogroup:admin"]},
"grants": [{"src": ["ada@example.com"], "dst": ["tag:server"], "ip": ["tcp:22"]}]
```

- `tag:server` is a name for all your servers.
- `src` is you: your login, or your laptop's tag, like `tag:laptop`.
- `tcp:22` is SSH, and nothing else.

Then give the server the tag: Machines, the server, the ⋯ menu, Edit ACL tags, `tag:server`. When the rules keep you out, moin prints this fix itself, with your names in it.

## Key expiry

Each device in a tailnet has a key. By default, it expires after some months. Then the device has to log in again. For a server, that means: SSH stops.

Two ways out:

- **Tag the server.** A tagged device's key does not expire.
- **Turn key expiry off.** Admin console, Machines, the server, the ⋯ menu, Disable key expiry.

Two weeks before a key expires, the server alerts, and `moin` shows it.

## Locked out?

Your provider's web console still works. Log in there and run:

```bash
/usr/local/bin/moin unlock
```

Port 22 opens to the internet again. [The way back in](/docs/security#the-way-back-in)
