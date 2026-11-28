---
title: "Security"
description: "Who can command your server. SSH only over Tailscale. Tailnet Lock. Cloudflare Tunnel. The firewall. The way back in. Updates you run yourself."
group: "Servers and data"
order: 2
---

Your server has doors to the internet, called ports. Each open door is a place to knock.

moin keeps the doors shut. And no service holds a key to your server: only you do.

## Who can command your server

Whoever can SSH to it as root. Nobody else.

- Not a watcher, not moin cloud, not us. They only receive reports, and moin cloud copies.
- The daemon acts on nothing a watcher answers, with one exception. After a copy, moin cloud answers with one address to upload that copy to. The daemon uses it only over https, only to a public address, for one PUT, and follows no redirect.
- Every SSH user who may run moin is as strong as root on that server. For a user who is not root, moin runs `sudo` by itself. That needs sudo without a password.
- With `moin follow`, whoever can push to the branch it follows can deploy too. [Deploy from CI](/docs/deploy#deploy-from-ci)

## The team

Your team is the SSH keys on the server. Whoever has a key there is on the team. More in [Teams](/docs/teams).

```bash
moin keys my-server
moin keys add my-server anna.pub
moin keys rm my-server anna@laptop
```

- `moin keys my-server` lists the keys that may log in as root, by name and fingerprint. They are the lines of `/root/.ssh/authorized_keys`.
- `moin keys add` adds a public key, from its `.pub` file. A private key never leaves your laptop: moin refuses one.
- A key's name is the comment at its end, like `anna@laptop`. `moin keys rm` takes it by that name, or by its fingerprint. Anna can't log in from then on.
- `moin keys rm` never takes out the key you are logged in with, so you can't lock yourself out.

With Tailscale, your tailnet's rules decide too: who may reach the server at all. There are no moin accounts.

## SSH only over Tailscale

A fresh server only answers SSH on public port 22. So the first deploy goes that way. Right after it, moin asks:

```text
? Close public SSH, only Tailscale from now on? [Y/n]
```

Enter means yes. Each step shows as it runs: putting the server in your tailnet, a new SSH session over Tailscale, closing the ports, one more session. Then:

1. moin puts the server in your tailnet, your own private network. Paste a Tailscale auth key, or press Enter for a link to log in.
2. It opens a new SSH session over Tailscale. The server must show the same host key as on its public address.
3. Only when that works, the server closes public port 22. First it sets a guard: in 2 minutes it opens port 22 again by itself.
4. moin opens one more new SSH session over Tailscale. That session runs `moin here secure confirm`, which stops the guard. If it never comes, the guard opens port 22 again. So a change that would lock you out undoes itself.
5. moin prints the line for your `~/.ssh/config`. It never edits that file itself:

```text
  Point my-server at the tailnet. In ~/.ssh/config, set:
    Host my-server
      HostName my-server.tail0000.ts.net
```

From then on, `moin deploy` goes over Tailscale. To the rest of the internet, port 22 is gone.

- It needs a Tailscale account. The free plan works.
- sshd must listen on port 22. If it listens on another one, moin leaves public SSH open.
- Tailscale keeps `41641/udp` open, for direct connections. If it is closed, Tailscale still works, through its relays.
- Said no? moin says `Port 22 stays open to the internet. Close it later with moin secure my-server`, and `moin` shows a `!` line until it is closed: `my-server: port 22 is open to the internet.`
- A server's Tailscale key expires, and then SSH to it stops. Two weeks before, the server alerts and `moin` shows it. Better: turn key expiry off for the server. Tailscale admin console, Machines, the server, the ⋯ menu, Disable key expiry.

## Tailnet Lock

Turn it on. With [Tailnet Lock](https://tailscale.com/kb/1226/tailnet-lock), a new device joins your tailnet only when one of your own devices signs it. Not even Tailscale can add one.

Without it, whoever controls your Tailscale account could add a device to your tailnet, and reach your server's SSH.

## Cloudflare Tunnel and the firewall

```bash
moin secure my-server
```

It closes public SSH as above, then asks for the token of a Cloudflare Tunnel. Make one in the Cloudflare dashboard: Networks, Tunnels, Create a tunnel, Cloudflared. The token is the long text after `--token`. Enter without one keeps the web on ports 80 and 443.

This closes the last open ports:

- **Web through Cloudflare Tunnel.** Visitors come through Cloudflare. Ports 80 and 443 close. Cloudflare serves HTTPS.
- **A firewall around the rest.** moin adds its own table to nftables, the firewall built into Linux. It lets in what the server needs, and drops everything else.

After it, no public TCP port is left on the server.

- It needs a Cloudflare account with your domain in it.
- Cloudflare sees the web traffic it carries. That is the trade.
- The instant address, `*.sslip.io`, needs ports 80 and 443. Behind the tunnel, use your own domain.
- Your provider may have a cloud firewall in front of the server. moin does not touch it.
- Ports your apps publish, like `5432` in a `compose.yml`, are closed to the internet too. Your tailnet still reaches them. Without a tunnel, 80 and 443 stay open for the web.
- The tunnel takes the web only once it reaches Cloudflare. With a wrong token, the web stays where it was.
- In the tunnel's Public Hostnames, send each domain to `http://moin-traefik:8080`.
- `moin secure my-server --tunnel off` takes the web out of the tunnel. It comes on ports 80 and 443 again.

### A server without apps

A server can run moin with no app, only for the firewall, the knocks and the reports. `moin servers add my-server` sets it up. It starts no proxy, so nothing listens on 80 and 443. `moin secure my-server` then closes public SSH and every other port, and asks for no tunnel. The first deploy starts the proxy and opens 80 and 443.

## Knocks

Bots try every new server within minutes: SSH, databases, admin pages. Each connection attempt is a knock. Once public SSH is closed, moin's firewall drops them, and counts each one under the name of its port.

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

- Without a server, it shows every server, one after the other. `--json` prints the numbers for a script.
- Ports 80 and 443 are not counted: who comes there is a visitor.
- Until public SSH is closed, nothing is counted.
- Every report tells your watchers the knocks of the day.

## A second wall

moin's firewall closes every port on the server and counts the knocks. For defense in depth, put your provider's firewall in front of it too.

- Let in Tailscale, UDP port 41641, and ping.
- With a Cloudflare Tunnel, let in nothing else. Without one, also TCP ports 80 and 443 for the web.
- The trade: a knock your provider drops never reaches the server, so moin cannot count it.

## The way back in

Lost your laptop, or your tailnet? Use your provider's web console. It is a screen and a keyboard on the server, in your browser.

Log in there as root and run:

```bash
/usr/local/bin/moin unlock
```

It opens public port 22 again, and starts sshd if it is off. Then SSH works as on day one. The full path also works with `sudo`, which may not look in `/usr/local/bin`.

Otherwise no port opens by itself. Only the guard does, in the 2 minutes after a change that did not let you back in.

`moin daemon uninstall` without `--apps` opens port 22 too, and takes the firewall away: `moin unlock` goes with moin, so nothing may stay closed behind it. [Remove moin](/docs/cli#remove-moin-from-a-server)

## Troubleshooting

**This computer does not reach the server over Tailscale.** moin says so, and public SSH stays open. Turn Tailscale on here, in the same tailnet, then `moin secure my-server` again.

**Both are in the tailnet, but port 22 does not answer.** Your tailnet's rules keep them apart. moin prints the fix, with the name your rules know this computer by. In the Tailscale admin console, Access controls, add a tag for servers and let this computer reach it on port 22:

```json
"tagOwners": {"tag:server": ["autogroup:admin"]},
"grants": [{"src": ["ada@example.com"], "dst": ["tag:server"], "ip": ["tcp:22"]}]
```

The source is this computer: its tags if it has any, like `tag:laptop`, else your login. Then give the server the tag: Machines, the server, the ⋯ menu, Edit ACL tags, `tag:server`. Then `moin secure my-server` again. A tagged server's key does not expire.

**moin says "No new SSH session over Tailscale came through".** The change closed something that kept you out. Wait 2 minutes: the guard opens port 22 again by itself. Then `ssh my-server` over the public address works, and `moin secure my-server` tries again.

**`moin` says Tailscale does not run on the server.** SSH only comes through it. In your provider's web console, run `tailscale up`, or `/usr/local/bin/moin unlock`.

## Updates

Your server never updates by itself. Not moin, not ever.

```bash
moin update
moin update my-server
```

- `moin update` updates the CLI on your laptop.
- `moin update my-server` puts that same version on the server, over SSH. Your apps keep running.
- Once a day the CLI asks GitHub for the newest release, and says so when one is out. You decide when.
- `moin deploy` says so when the server runs another version than your CLI. The watcher's page shows the version of each server.

Each release is signed. moin checks the signature before it installs anything. The signing key never leaves a protected place in GitHub: only a release tag reaches it, and only after a maintainer approves.

- Every download comes from the GitHub release: the binaries, `SHA256SUMS`, and two signatures of it.
- `SHA256SUMS.sig` is for `moin update` and `moin update my-server`. They check it themselves.
- `SHA256SUMS.sshsig` is for `cli.sh`. It checks it with `ssh-keygen`, which every Mac and Linux has. It needs OpenSSH 8.1 or newer: `ssh -V` says which.
- Both are made with the same key. A spare key, kept offline, may sign one day instead.

Check a download by hand, the way `cli.sh` does:

```bash
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/moin-linux-amd64
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/SHA256SUMS
curl -fsSLO https://github.com/axadrn/moin/releases/download/v1.2.3/SHA256SUMS.sshsig
cat > allowed_signers <<'KEYS'
moin-release ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHZMF3btxHJfEbOLX18SK74qTn0WuUetbX1hHSiEsNyw
moin-release ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICtglRxa7QWCtSE+7Pvtv0fy2TcTuvoxj3OvBGYlAyoU
KEYS
ssh-keygen -Y verify -f allowed_signers -I moin-release -n moin-release -s SHA256SUMS.sshsig < SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
```

You want `Good "moin-release" signature`, then `OK` next to the binary. On a Mac, `shasum -a 256 --check --ignore-missing SHA256SUMS`. Anything else: do not run it.

## If a watcher is hacked

A watcher only listens. If someone takes it over:

- **They see** the names of your servers and apps, your domains, and when backups and checks ran.
- **They get** your alert channels: the Telegram bot token and chat, the Slack, Discord or webhook addresses, and for an email the SMTP server, user and password you set with `moin alerts add`. A watcher holds them so it can alert you when the server can't.
- **They can** post to those channels, and send mail as that SMTP user. Nothing more: a channel reaches no server. So give email alerts an SMTP user of their own, one that can only send. Make a new bot token, webhook or SMTP password, then `moin alerts rm` the old one and `moin alerts add` the new.
- **They can** silence its alerts. So run two watchers, best at two providers.
- **They can't** reach any server. A daemon sends reports out, and acts on nothing that comes back. The one exception is the upload address of a copy, above: the most a false one gets is an encrypted copy.

What a watcher holds:

- **Your own watcher** keeps the channels in its state file, `/var/lib/moin/watcher/state.json`. Only its owner may read it.
- **moin cloud** keeps them encrypted at rest, with AES-GCM. The key is not in the database.
- **Both** send a server's alerts only to public addresses: a webhook over `https://`, an email through a public SMTP server. They follow no redirect. A report can't make a watcher knock on its own network.
- **An email** goes out only after the SMTP server turned on TLS with STARTTLS. Without it nothing is sent, the password least of all.
- **On the way**, a report goes only over HTTPS, or inside your tailnet.

## If a server is hacked

Then they are root on it, as you are. They can read its apps, its data and its env vars. They can also use its bucket key, and that key can delete copies.

That is why moin cloud keeps a second copy the server can only add to, never delete. [moin cloud](/docs/cloud#a-copy-your-server-cant-delete)

## Where your secrets live

On your server, and only there. Root only.

| File | What is in it |
| --- | --- |
| `/var/lib/moin/apps/<app>/app.env` | the env vars of one app |
| `/etc/moin/config.yml` | the bucket key, the alert tokens and SMTP passwords, the watcher tokens |
| `/etc/moin/backup.key` | the server's own backup key, which opens its copies |
| `/var/lib/moin/server.json` | the Cloudflare Tunnel token, and what is closed |
| `/var/lib/moin/follow/<app>/key` | the read-only deploy key of an app that follows a branch |
| `/root/.docker/config.json` | the logins of the private registries, from `moin registry set`. Mode 600 |

They reach the server over your SSH connection, and nothing else. No copy of them is kept anywhere but on the server.
