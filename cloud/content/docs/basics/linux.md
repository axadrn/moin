---
title: "Linux and the terminal"
description: "Your server has no screen. You talk to it by typing. The ten Linux commands you need to look around, and what each one shows."
group: "Basics"
order: 2
---

Your server runs Linux. It has no screen and no mouse. You talk to it by typing, in a terminal.

You type a command. You press Enter. The server answers with text. That is all a terminal is.

moin does the hard parts for you. These ten commands are for looking around.

## Get in

From your laptop, open the terminal app and log in:

```bash
ssh prod
```

The prompt changes. It now shows the server's name. Every command you type runs there, not on your laptop. `exit` takes you back. [SSH and keys](/docs/basics/ssh) sets up the name `prod`.

## The ten commands

**1. `ls`** lists what is in a folder.

```bash
ls
ls -la /etc/moin
```

`-la` shows every file, the hidden ones too, with its owner and size.

**2. `cd`** goes into a folder.

```bash
cd /var/lib/moin
cd ..
cd
```

`..` is one folder up. `cd` alone goes home.

**3. `cat`** prints a file.

```bash
cat /etc/os-release
```

That one says which Linux runs.

**4. `nano`** edits a file. It is the simplest editor.

```bash
nano notes.txt
```

Type. Then Ctrl+O and Enter saves. Ctrl+X quits. The keys stand at the bottom of the screen.

**5. `sudo`** runs one command as root.

```bash
sudo cat /etc/moin/config.yml
```

Root may do everything. Other users may not. Logged in as root, you need no `sudo`. As another user, put it in front.

**6. `systemctl`** starts, stops and shows services. A service is a program that runs all the time, and starts again after a reboot.

```bash
systemctl status moin-daemon
sudo systemctl restart moin-daemon
```

`moin-daemon` is moin's service on your server. It runs the backups and the checks. `q` leaves the status.

**7. `journalctl`** shows what a service wrote to its log.

```bash
journalctl -u moin-daemon -n 50
journalctl -u moin-daemon -f
```

`-n 50` is the last 50 lines. `-f` follows new ones, until Ctrl+C.

**8. `df`** shows how full the disks are.

```bash
df -h
```

`-h` writes sizes like a human: `12G`, not a long number. Look at the line for `/`. Full disks break things, so moin alerts at 90%.

**9. `free`** shows the memory.

```bash
free -h
```

`available` is what is left for new programs. Near zero, and a build may fail.

**10. `docker ps`** lists the containers that run.

```bash
docker ps
```

Each app runs in one or more containers. You see their names, their images and how long they have run. As another user than root, it is `sudo docker ps`. [Docker and compose](/docs/basics/docker)

## Keys that help

- **Tab** finishes a name for you. Type `/var/li` and press Tab.
- **Up arrow** brings back the last command.
- **Ctrl+C** stops what runs now.
- **Ctrl+L** clears the screen.

## What you rarely need

With moin you seldom log in at all. The CLI on your laptop does it for you:

```bash
moin logs shop
moin exec shop -- sh
```

`moin logs` shows the app's logs. `moin exec` runs a command in the app's container, here a shell. [Deploy](/docs/deploy#logs)

Next: [SSH and keys](/docs/basics/ssh), how your laptop gets in.
