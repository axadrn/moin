---
title: "Security"
description: "Who can command your server. SSH only over Tailscale, Tailnet Lock, Cloudflare Tunnel, the firewall, the way back in, and updates you run yourself."
group: "Servers and data"
order: 2
---

Your server has doors to the internet, called ports. Each open door is a place to knock.

moin keeps the doors shut. And no service holds a key to your server: only you do.

## Who can command your server

Whoever can SSH to it as root. Nobody else.

- Not a watcher, not moin cloud, not us. They only receive reports.
- Every SSH user who may run moin is as strong as root on that server. For a user who is not root, moin runs `sudo` by itself. That needs sudo without a password.
- Coming: `moin follow`. Then whoever can push to the branch it follows can deploy too.

## The team

Your team is the SSH keys on the server. Whoever has a key there is on the team.

```bash
moin keys prod
moin keys add prod anna.pub
moin keys rm prod anna@laptop
```

- `moin keys prod` lists the keys that may log in as root, by name and fingerprint. They are the lines of `/root/.ssh/authorized_keys`.
- `moin keys add` adds a public key, from its `.pub` file. A private key never leaves your laptop: moin refuses one.
- A key's name is the comment at its end, like `anna@laptop`. `moin keys rm` takes it by that name, or by its fingerprint. Anna can't log in from then on.
- `moin keys rm` never takes out the key you are logged in with, so you can't lock yourself out.

With Tailscale, your tailnet's rules decide too: who may reach the server at all. There are no moin accounts.

## SSH only over Tailscale

Coming. A fresh server only answers SSH on public port 22. So the first deploy goes that way. Right after it, moin asks:

```text
Close public SSH, only Tailscale from now on? [Y/n]
```

Enter means yes. Then:

1. moin puts the server in your tailnet, your own private network.
2. It opens a new SSH session over Tailscale.
3. Only when that works, it closes public port 22.
4. It points your `prod` alias at the server's tailnet name.

From then on, `moin deploy` goes over Tailscale. To the rest of the internet, port 22 is gone.

- It needs a Tailscale account. The free plan works.
- With Tailscale SSH, the server needs no SSH daemon at all.
- Tailscale keeps `41641/udp` open, for direct connections. If it is closed, Tailscale still works, through its relays.
- Said no? `moin` shows a red line until it is closed: `prod: port 22 open to the internet`.
- A server's Tailscale key expires. A watcher warns you before it does.

## Tailnet Lock

We strongly recommend it. With [Tailnet Lock](https://tailscale.com/kb/1226/tailnet-lock), a new device joins your tailnet only when one of your own devices signs it. Not even Tailscale can add one.

Without it, whoever controls your Tailscale account could add a device to your tailnet, and reach your server's SSH.

## Cloudflare Tunnel and the firewall

Coming.

```bash
moin secure prod
```

This closes the last open ports:

- **Web through Cloudflare Tunnel.** Visitors come through Cloudflare. Ports 80 and 443 close. Cloudflare serves HTTPS.
- **A firewall around the rest.** moin adds its own table to nftables, the firewall built into Linux. It lets in what the server needs, and drops everything else.

After it, no public TCP port is left on the server.

- It needs a Cloudflare account with your domain in it.
- Cloudflare sees the web traffic it carries. That is the trade.
- The instant address, `*.sslip.io`, needs ports 80 and 443. Behind the tunnel, use your own domain.
- Your provider may have a cloud firewall in front of the server. moin does not touch it.

## Knocks

Bots try every new server within minutes: SSH, databases, admin pages. Each try is a knock. Once public SSH is closed, moin's firewall drops them, and counts each one under the name of its port.

```bash
moin knocks prod
```

```text
prod
PORT       HOUR   TODAY   7 DAYS   YEAR      TOTAL     PER MINUTE
ssh        47     1,120   7,904    131,220   240,017   0.8
postgres   6      142     1,003    17,436    30,551    0.1
all        71     1,843   12,880   212,904   389,117   1.2
All dropped. Counted since March 2, 2026.
```

- Without a server, it shows every server, one after the other. `--json` prints the numbers for a script.
- Ports 80 and 443 are not counted: who comes there is a visitor.
- Until public SSH is closed, nothing is counted.
- Every report tells your watchers the knocks of the day.

## The way back in

Coming, with `moin secure`. Lost your laptop, or your tailnet? Use your provider's web console. It is a screen and a keyboard on the server, in your browser.

Log in there as root and run:

```bash
moin unlock
```

It opens public port 22 again. Then SSH works as on day one. No port ever opens by itself: only `moin unlock`, typed on the server.

## Updates

Your server never updates by itself. Not moin, not ever.

```bash
moin update
moin update prod
```

- `moin update` updates the CLI on your laptop.
- `moin update prod` puts that same version on the server, over SSH. Your apps keep running.
- Once a day the CLI asks GitHub for the newest release, and says so when one is out. You decide when.
- `moin deploy` says so when the server runs another version than your CLI. The watcher's page shows the version of each server.

Each release is signed. moin checks the signature before it installs anything. The signing key never leaves a protected place in GitHub: only a release tag reaches it, and only after a maintainer approves.

Check a download by hand:

```bash
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/moin-linux-amd64
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/SHA256SUMS
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/SHA256SUMS.sig
cat > moin-release.pem <<'KEY'
-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAdkwXdu3Ecl8Rs4tfXxIrvipOfRa5R61tfWEdKISw3LA=
-----END PUBLIC KEY-----
KEY
openssl pkeyutl -verify -pubin -inkey moin-release.pem -rawin -in SHA256SUMS -sigfile SHA256SUMS.sig
sha256sum --check --ignore-missing SHA256SUMS
```

You want `Signature Verified Successfully`, then `OK` next to the binary. Anything else: do not run it.

## If a watcher is hacked

A watcher only listens. If someone takes it over:

- **They see** the names of your servers and apps, your domains, and when backups and checks ran.
- **They can** silence its alerts. So run two watchers, best at two providers.
- **They can't** reach any server. A daemon sends reports out, and acts on nothing that comes back.

## If a server is hacked

Then they are root on it, as you are. They can read its apps, its data and its env vars. They can also use its bucket key, and that key can delete copies.

That is why moin cloud will keep a second copy the server can only add to, never delete. Coming. [moin cloud](/docs/cloud)

## Where your secrets live

On your server, and only there. Root only.

| File | What is in it |
| --- | --- |
| `/var/lib/moin/apps/<app>/app.env` | the env vars of one app |
| `/etc/moin/config.yml` | the bucket key, the alert tokens, the watcher tokens |

They reach the server over your SSH connection, and nothing else. No copy of them is kept anywhere but on the server.
