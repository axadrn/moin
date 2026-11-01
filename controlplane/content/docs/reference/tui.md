---
title: "Terminal UI"
description: "Every key of ops without a command"
order: 2
---

Run `ops` in a repo. Everything updates live. The activity tab shows who did what in your workspace.

The header shows the tabs and the control plane, the footer the keys of the
place you are in. A question like "Remove web-1?" waits there for `y`; any
other key cancels. An open app shows its deploys, what the agent detected and
a panel with the deploy log, the logs or the env vars. The server list shows
how each server is reached (ACCESS) and its own rules for ops (RULES), from
`/etc/ops/rules` on it. Notices clear with the next key.

| Key | Where | Does |
| --- | --- | --- |
| `tab` | everywhere | switches between servers, apps and activity |
| `↑` `↓` or `k` `j` | lists | selects |
| `a` | servers | adds a server and shows its install command |
| `t`, then `y` | servers | turns the Cloudflare Tunnel on or off |
| `f`, then `y` | servers | turns the firewall on or off |
| `x`, then `y` | servers, apps | removes the selected one |
| `enter` | apps | opens the app with its deploys and databases |
| `d` | apps | deploys the directory `ops` started in |
| `g` | apps | deploys the linked branch from GitHub |
| `R` | apps | redeploys the newest source or image |
| `i` | apps | deploys an image built in CI; type the image, then `enter` |
| `h` | apps | shows a new deploy hook URL for CI, once |
| `o` | apps | shows domains and DNS; type `blog.de`, `-blog.de` or `=blog.de` |
| `p` | apps | password: type `user:password`, or `off` |
| `r`, then `y` | apps | rolls back to the deploy before the live one |
| `l` | apps | shows the logs |
| `e` | apps | shows env keys; type `KEY=value` or `-KEY`, then `enter` |
| `s` | apps | shows the tables; type SQL, then `enter`. A change asks first, `y` runs it |
| `b` | apps | shows the backups; `enter` backs up now, a snapshot ID or a time like `14:01` restores after `y` |
| `esc` | app | back to the list |
| `q` | everywhere | quits |
