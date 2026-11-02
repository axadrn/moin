---
title: "Terminal UI"
description: "Every key of ops without a command"
order: 2
---

Run `ops` in a repo. Everything updates live. The activity tab shows who did what in your workspace, the team tab its members and open invitations, the settings tab what belongs to the workspace: the GitHub App, the bucket, the Cloudflare token, the Tailscale key and the registry logins.

The header shows the tabs and the control plane, the footer only the keys
that work in the place you are in. A question like "Remove web-1?" waits
there for `y`; any other key cancels. A form, such as a new app or the
bucket, asks its fields one after the other: `enter` takes the answer or
the value it names, `esc` cancels. An open app shows its deploys, what the
agent detected and a panel with a deploy log, the logs or the env vars. The
server list shows how each server is reached (ACCESS) and its own rules for
ops (RULES), from `/etc/ops/rules` on it. Notices clear with the next key.

The team tab does what an API token may: inviting, resending and revoking
invitations and renaming need an admin or the owner. Changing roles,
removing members, leaving and deleting the workspace are web only.

`esc` closes what is open and goes back; it never quits. Only `q` and
`ctrl+c` quit.

| Key | Where | Does |
| --- | --- | --- |
| `tab` | everywhere | switches between servers, apps, activity, team and settings |
| `↑` `↓` or `k` `j` | lists | selects |
| `a` | servers | adds a server and shows its install command |
| `t`, then `y` | servers | turns the Cloudflare Tunnel on or off |
| `s`, then `y` | servers | turns SSH only on Tailscale on or off |
| `u` | servers | updates the server's agent to the control plane's version |
| `f`, then `y` | servers | turns the firewall on or off |
| `x`, then `y` | servers, apps | removes the selected one; for a server, shows the command that removes its agent there |
| `a` | apps | creates an app: its name, then its server |
| `enter` | apps | opens the app with its deploys and databases |
| `↑` `↓` | app | picks a deploy; `enter` shows its log |
| `d` | apps | deploys the directory `ops` started in, when its app exists |
| `g` | apps | GitHub: shows the repo and the repos ops may read. `enter` deploys the linked branch, a branch, tag or commit deploys that, `link owner/repo [branch] [manual]` follows a repo (`manual`: no deploy on push), `unlink` stops |
| `R` | apps | redeploys the newest source or image |
| `t` | apps | restarts the app with the current env and domains |
| `S` | apps | stops the app, after `y`; starts a stopped one |
| `i` | apps | deploys an image built in CI; type the image, then `enter` |
| `h` | apps | asks first, then shows a new deploy hook URL for CI, once; the old one stops working |
| `o` | apps | shows domains and DNS; type `blog.de`, `-blog.de` or `=blog.de` |
| `p` | apps | password: type `user:password`, or `off` |
| `r`, then `y` | apps | rolls back to the picked deploy, or without a pick to the one before the live one |
| `l` | apps | shows the logs |
| `e` | apps | shows env keys; type `KEY=value` or `-KEY`, `show` or `hide` for the values, then `enter` |
| `s` | apps | shows the tables; type SQL, then `enter`. A change asks first, `y` runs it |
| `b` | apps | shows the backups; `enter` backs up now, a snapshot ID or a time like `14:01` restores after `y`, `on` or `off` turns the snapshot before every change on or off |
| `a` | team | invites someone: the email, then `member` or `admin` |
| `enter` | team | sends the selected invitation again |
| `x`, then `y` | team | revokes the selected invitation |
| `n` | team | renames the workspace |
| `enter` | settings | sets the selected one: the bucket, a key or a registry login; on GitHub it shows the install link |
| `a` | settings | adds a registry login |
| `x`, then `y` | settings | removes the bucket, a key or a registry login |
| `ctrl+u` | inputs | clears what you typed; pasted text goes in on one line |
| `esc` | app | drops the pick, then back to the list |
| `q` | everywhere | quits |
