---
title: "Terminal UI"
description: "Every key of ops without a command"
order: 2
---

Run `ops` in a repo. Everything updates live. The activity tab shows who did what in your workspace, to the second in your time zone, the team tab its members and open invitations, the settings tab what belongs to the workspace: the version of the control plane, agent updates, your email alerts, the alert destinations, the GitHub App, the bucket, the Cloudflare token, the Tailscale key and the registry logins. When a newer release of a self-hosted control plane is out, the status line says so.

The header shows the tabs and the control plane, the footer only the keys
that work in the place you are in. A question like "Stop <app-name>?" waits
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

`esc` closes what is open and goes back; it never quits. Only `q`, `ZZ`,
`:q` and `ctrl+c` quit.

## Vim keys

The TUI moves like vim, and `?` shows every key.

- **Move.** `j` `k` select, with a count like `5j`. `gg` and `G` go to the
  first and the last row, `5G` or `:5` to row 5. `ctrl+d` `ctrl+u` move half
  a page, `ctrl+f` `ctrl+b` a page. `l` goes into an app or a deploy's log,
  `h` back out.
- **Tabs.** `gt` and `tab` go to the next tab, `gT` and `shift+tab` to the
  one before, `3gt` to the third.
- **Search.** `/word`, then `enter`, finds a row, or a line in the logs. Case
  counts only when the word has a capital letter. `n` and `N` find the next
  and the one before, around the end; `:noh` forgets the search.
- **Command line.** `:` runs any key by its name, on the place you are:
  `:deploy`, `:restart`, `:logs`, `:hook off`, `:rename new-name`. `:e shop`
  opens an app, `:apps` and the other tab names switch the tab. `tab`
  completes a command.
- **Logs.** `L` opens the logs, ready to scroll: `j` `k`, `ctrl+d` `ctrl+u`,
  `gg` for the top. New lines keep coming without moving what you read; the
  panel says how many are below, and `G` follows the end again. `ctrl+w`
  moves between the list and the logs, `esc` back to the list.
- **Inputs.** `ctrl+u` clears the line, `ctrl+w` deletes the last word.

| Key | Where | Does |
| --- | --- | --- |
| `tab` or `gt` | everywhere | switches between servers, apps, activity, team and settings; `shift+tab` or `gT` goes back |
| `↑` `↓` or `k` `j` | lists | selects; `gg` `G` the first and last, `ctrl+d` `ctrl+u` half a page |
| `/`, then `n` `N` | lists, logs | searches |
| `:` | everywhere | the command line: `:q`, `:5`, `:e <app>`, `:<key name>` |
| `?` | everywhere | shows every key |
| `a` | servers | adds a server and shows its install command |
| `t`, then `y` | servers | turns the Cloudflare Tunnel on or off |
| `s`, then `y` | servers | turns SSH only on Tailscale on or off |
| `cw` or `:rename` | servers | renames the selected server |
| `K` `J` or `:move up` `:move down` | servers, apps | moves the selected server or app up or down in the order every list shows |
| `i` | servers | shows a new one-time install command for the selected server; the old one stops working |
| `u` | servers | updates the server's agent to the control plane's version |
| `m` or `:metrics` | servers, apps | shows CPU, memory, disk and network now with a sparkline of the last hour, and for a server its apps by memory; the panel loads them again every 15 seconds while it shows them |
| `f`, then `y` | servers | turns the firewall on or off |
| `x`, then the name | servers, apps | removes the selected one; for a server, shows the command that removes its agent there |
| `a` | apps | creates an app: its name, then its server |
| `enter` or `l` | apps | opens the app with its deploys and databases |
| `j` `k` | app | picks a deploy; `enter` or `l` shows its log |
| `d` | apps | deploys the directory `ops` started in, when its app exists |
| `gh` | apps | GitHub: shows the repo and the repos ops may read. `enter` deploys the linked branch, a branch, tag or commit deploys that, `link owner/repo [branch] [manual]` follows a repo (`manual`: no deploy on push), `branches owner/repo` lists its branches, `unlink` stops |
| `R` | apps | redeploys the newest source or image |
| `t` | apps | restarts the app with the current env and domains |
| `S` | apps | stops the app, after `y`; starts a stopped one |
| `i` | apps | deploys an image built in CI; type the image, then `enter` |
| `:hook` | apps | asks first, then shows a new deploy hook URL for CI, once; the old one stops working |
| `:hook off`, then `y` | apps | turns the deploy hook off; CI can no longer deploy |
| `o` | apps | shows domains and DNS; type `<domain>` to add, `-<domain>` to remove (asks first) or `=<domain>` for the canonical one |
| `p` | apps | password: type `user:password`, or `off` (asks first) |
| `u` or `:uptime` | apps | shows the uptime of every domain and the last outages; `enter` checks again, `pause <domain>` or `resume <domain>` pauses or resumes a check, `<domain> /health` sets its path, `<domain> default` goes back to the health path or `/` |
| `r`, then `y` | apps | rolls back to the picked deploy, or without a pick to the one before the live one |
| `L` | apps | shows the logs, ready to scroll and search; `G` follows the end |
| `e` | apps | shows env keys; type `KEY=value` or `-KEY` (asks first), `show` or `hide` for the values, or `edit` for all of them as one `.env` in `$EDITOR`, then `enter` |
| `s` | apps | shows the tables; type SQL, then `enter`. A change asks first, `y` runs it |
| `b` | apps | shows the backups; `enter` backs up now, a snapshot ID or a time like `14:01` restores once you typed the app's name, `on` or `off` turns the snapshot before every change on or off |
| `f` | activity | filters by app, server or member; empty fields keep everything |
| `c` | activity | clears the filter |
| `m` | activity | loads older events |
| `a` | team | invites someone: the email, then `member` or `admin` |
| `enter` | team | sends the selected invitation again |
| `x`, then `y` | team | revokes the selected invitation |
| `cw` or `:rename` | team | renames the workspace |
| `enter` | settings | sets the selected one: the bucket, a key or a registry login; on GitHub it shows the install link |
| `u` or `:update` | settings | updates a self-hosted control plane to the newest release, after `y`; for its admins |
| `A` or `:auto-update` | settings | turns automatic agent updates on or off; `enter` on **Agent updates** does the same |
| `E` or `:alerts` | settings | turns your [email alerts](/docs/guides/alerts) on or off; `enter` on **Email me alerts** does the same |
| `C` or `:alert` | settings | adds an [alert destination](/docs/guides/alerts#slack-discord-telegram-and-webhooks): the kind, a name, the URL or Telegram bot token, the Telegram chat and the events |
| `T`, `:test` or `enter` | settings | sends a test alert to the selected destination |
| `a` | settings | adds a registry login |
| `x`, then `y` | settings | removes a key, an alert destination or a registry login; the bucket asks for its name |
| `ctrl+u` | inputs | clears what you typed; pasted text goes in on one line |
| `ctrl+w` | inputs | deletes the last word |
| `enter` | protect, GitHub, domains | sends what you typed; if ops refuses it, the input stays open with your text and the reason, to fix and send again |
| `esc` or `h` | app | drops the pick, then back to the list |
| `q`, `ZZ` or `:q` | everywhere | quits |
