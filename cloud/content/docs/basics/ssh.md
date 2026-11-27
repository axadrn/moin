---
title: "SSH and keys"
description: "SSH is how your laptop logs in to your server. Make a key, give the server its public half, and name the server prod in ~/.ssh/config."
group: "Basics"
order: 3
---

SSH is a safe line from your laptop to your server. Everything on it is encrypted. moin sends every command over it.

To get in, you need a key. Not a password.

## A key has two halves

```text
your laptop                         your server
┌──────────────────┐                ┌──────────────────────────┐
│ id_ed25519       │                │ /root/.ssh/              │
│ private: secret  │ ── proves ──▶  │   authorized_keys        │
│                  │                │   id_ed25519.pub: public │
│ id_ed25519.pub   │                │                          │
└──────────────────┘                └──────────────────────────┘
```

- The **private key** stays on your laptop. It is like the key to your front door. Never give it to anyone. Never paste it anywhere.
- The **public key** goes on the server. It is like the lock. Anyone may see it. It only opens for your private key.

When you log in, your laptop proves it has the private key. It never sends it.

## Make a key

On your laptop:

```bash
ssh-keygen -t ed25519 -C "ada@laptop"
```

- `-t ed25519` is the kind of key. Short and strong.
- `-C` is a comment, so you know later whose key it is.

Press Enter to keep the place, `~/.ssh/id_ed25519`. Then it asks for a passphrase. Type one. It locks the key on your disk, so a stolen laptop is not a stolen server. Your Mac keychain or `ssh-agent` remembers it, so you don't type it every time.

Two files appear:

```text
~/.ssh/id_ed25519       the private key
~/.ssh/id_ed25519.pub   the public key
```

Show the public one:

```bash
cat ~/.ssh/id_ed25519.pub
```

It is one line that starts with `ssh-ed25519`. Paste that line into your provider's form when you make the server. The provider puts it on the server for you.

## Log in once

```bash
ssh root@203.0.113.7
```

The first time, SSH asks:

```text
The authenticity of host '203.0.113.7' can't be established.
Are you sure you want to continue connecting (yes/no/[fingerprint])?
```

Type `yes`. SSH notes the server's own key in `~/.ssh/known_hosts`. Next time, it checks that it talks to the same server. If that key ever changes, SSH warns you loudly. Stop, and find out why.

`exit` logs you out.

## Give the server a name

Typing the IP each time is tiring. Give the server a short name, in `~/.ssh/config` on your laptop:

```text
Host prod
  HostName 203.0.113.7
  User root
  IdentityFile ~/.ssh/id_ed25519
```

- `Host` is the name you pick. Here `prod`.
- `HostName` is the server's address.
- `User` is who you log in as.
- `IdentityFile` is your private key.

Now this is enough:

```bash
ssh prod
```

This name is what moin uses. If `ssh prod` works, `moin deploy prod` works. moin reads the same file, and goes the same way.

Not root? Set `User` to your user. moin then runs `sudo` for you. It needs sudo without a password.

## More people, more keys

Each person makes their own key on their own laptop. Never share one key.

```bash
moin keys prod
moin keys add prod anna.pub
moin keys rm prod anna@laptop
```

`moin keys add` takes Anna's public key, from her `.pub` file. A private key never leaves a laptop: moin refuses one. [The team](/docs/security#the-team)

## If it goes wrong

- **`Permission denied (publickey)`.** The server does not know this key. Check that the `.pub` line is on the server, and that `IdentityFile` names the right file.
- **Lost the laptop.** Take its key out from another laptop: `moin keys rm prod ada@laptop`. Or in your provider's web console.
- **The private key leaked.** Same: take it out now, and make a new one.
