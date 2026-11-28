---
title: "Ports and the firewall"
description: "A port is a door of your server. Bots knock within minutes. Which doors a fresh server has open. Why Docker walks past ufw. What moin closes."
group: "Basics"
order: 4
---

Your server has one address. Behind it are many doors. Each door has a number. That number is a port.

A program opens a door to listen. Visitors knock on that door to reach it.

```text
203.0.113.7
├── 22    SSH, for you
├── 80    the web, plain
├── 443   the web, with HTTPS
└── 5432  Postgres, if it listens there
```

A few numbers are a habit. 22 is SSH. 80 and 443 are the web. 5432 is Postgres. Any program can pick any free one.

## What is open on a fresh server

Most providers give you a server with port 22 open, for SSH. That is how you get in.

Nothing guards the rest. There is no firewall yet. Every program that listens on the public address can be reached from the whole internet. Install a database that listens there, and the world can knock on it.

## Bots knock within minutes

Bots try every address on the internet, all day. A new server gets its first knocks within minutes. They try SSH passwords. They try databases. They try admin pages.

They don't pick you. They try everyone. moin counts the knocks once public SSH is closed:

```bash
moin knocks my-server
```

```text
my-server
PORT       HOUR   TODAY   7 DAYS   YEAR      TOTAL     PER MINUTE
ssh        47     1,120   7,904    131,220   240,017   0.8
postgres   6      142     1,003    17,436    30,551    0.1
all        71     1,843   12,880   212,904   389,117   1.2
Connection attempts, all dropped. Counted since March 2, 2026.
```

That is a normal server. It is the noise of the internet. [Knocks](/docs/security#knocks)

## Why close them

Each open door is a chance. A bug in the program behind it. A weak password. A setting you forgot. A closed door has none of these.

So the rule is simple: open only what visitors need. For a website, that is 80 and 443. Everything else stays shut.

## The firewall

A firewall is the guard at the doors. It lets in what you allow and drops the rest.

On Ubuntu, the common one is `ufw`:

```bash
ufw default deny incoming
ufw allow 22
ufw allow 80
ufw allow 443
ufw enable
```

That looks safe. It has one big hole.

## Docker walks past ufw

A port a container publishes skips ufw. Like this one, in a `compose.yml`:

```yaml
services:
  db:
    image: postgres:17
    ports:
      - "5432:5432"
```

Port 5432 is now open to the whole internet, even with `ufw default deny incoming`.

Why? Docker writes its own firewall rules. Traffic for a container is sent on to it. It never passes the place where ufw looks. So ufw says "closed", and the door is open.

The fix is to not publish what only your app needs. Two containers in one compose file reach each other by name, like `db:5432`, without `ports`.

## What moin does

moin brings its own firewall. It is one table in nftables, the firewall built into Linux. It guards the server itself, and the ports your containers publish. Docker can't walk past it.

- **After your first deploy**, moin asks to close public SSH. Port 22 then answers only over Tailscale, your private network. [Tailscale](/docs/basics/tailscale)
- **With `moin secure my-server`**, the firewall closes every other port too. Published ports like `5432` close to the internet. Your tailnet still reaches them.
- **With a Cloudflare Tunnel**, 80 and 443 close as well. Then no public TCP port is left. [Cloudflare Tunnel](/docs/basics/cloudflare-tunnel)

Tailscale keeps `41641/udp` open, for direct connections. It works without it too, a little slower.

You need no ufw next to moin's firewall.

Your provider may also have a cloud firewall, in its website, in front of the server. moin does not touch it. If you use one, let in 80 and 443 while the web comes that way.

## Locked out?

Every change of moin's that could lock you out undoes itself after 2 minutes, unless a new SSH session proves you still get in. And your provider's web console always works:

```bash
/usr/local/bin/moin unlock
```

It opens port 22 again. [The way back in](/docs/security#the-way-back-in)
