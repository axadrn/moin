---
title: "Terminal UI"
description: "Every key of ops without a command"
order: 2
---

Run `ops` in a repo. Everything updates live. The activity tab shows who did what in your workspace, to the second in your time zone, the team tab its members and open invitations, the settings tab what belongs to the workspace: the GitHub App, the bucket, the Cloudflare token, the Tailscale key and the registry logins.

The header shows the tabs and the control plane, the footer only the keys
that work in the place you are in. A question like "Stop blog?" waits
there for `y`; any other key cancels. Removing a server, an app or the
bucket and a restore cannot be undone, so they ask you to type the name
first, then `enter`; you can paste it. A form, such as a new app or the
bucket, asks its fields one after the other: `enter` takes the answer or
the value it names, `esc` cancels. An open app shows its deploys, what the
agent detected and a panel with a deploy log, the logs or the env vars. The
server list shows how each server is reached (ACCESS) and its own rules for
ops (RULES), from `/etc/ops/rules` on it. Notices clear with the next key.

The team tab does what an API token may, like `ops team`: inviting,
resending and revoking invitations and renaming need an admin or the owner. Changing roles,
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
| `n` | servers | renames the selected server |
| `i` | servers | shows a new one-time install command for the selected server; the old one stops working |
| `u` | servers | updates the server's agent to the control plane's version |
| `f`, then `y` | servers | turns the firewall on or off |
| `x`, then the name | servers, apps | removes the selected one; for a server, shows the command that removes its agent there |
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
| `H`, then `y` | apps | turns the deploy hook off; CI can no longer deploy |
| `o` | apps | shows domains and DNS; type `blog.de`, `-blog.de` (asks first) or `=blog.de` |
| `p` | apps | password: type `user:password`, or `off` (asks first) |
| `r`, then `y` | apps | rolls back to the picked deploy, or without a pick to the one before the live one |
| `l` | apps | shows the logs |
| `e` | apps | shows env keys; type `KEY=value` or `-KEY` (asks first), `show` or `hide` for the values, then `enter` |
| `s` | apps | shows the tables; type SQL, then `enter`. A change asks first, `y` runs it |
| `b` | apps | shows the backups; `enter` backs up now, a snapshot ID or a time like `14:01` restores once you typed the app's name, `on` or `off` turns the snapshot before every change on or off |
| `f` | activity | filters by app, server or member; empty fields keep everything |
| `c` | activity | clears the filter |
| `m` | activity | loads older events |
| `a` | team | invites someone: the email, then `member` or `admin` |
| `enter` | team | sends the selected invitation again |
| `x`, then `y` | team | revokes the selected invitation |
| `n` | team | renames the workspace |
| `enter` | settings | sets the selected one: the bucket, a key or a registry login; on GitHub it shows the install link |
| `a` | settings | adds a registry login |
| `x`, then `y` | settings | removes a key or a registry login; the bucket asks for its name |
| `ctrl+u` | inputs | clears what you typed; pasted text goes in on one line |
| `esc` | app | drops the pick, then back to the list |
| `q` | everywhere | quits |
