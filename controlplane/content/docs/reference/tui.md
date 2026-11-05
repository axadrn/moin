---
title: "Terminal UI"
description: "Every view and key of moin without a command"
order: 2
---

Run `moin` in a repo. The terminal UI is for the daily work: see what runs, deploy, read the logs, roll back, back up and restore, change env vars, check domains, uptime and metrics, run a command. Everything updates live, and nothing waits for a spinner: `moin` starts with what it showed last time, marked **last seen** in the header until the control plane answers a moment later, and an app you come back to shows its tabs at once while they load again. It keeps that in your cache folder, readable only by you, without env values, logs or the token; `moin logout` removes it. Setup you do once, like billing, the team or a new server, opens the web app in your browser. [Where things work](/docs/reference/surfaces) lists what each place can do.

Without a token, `moin` signs you in first: it shows a code, opens the browser to confirm it and lands on the dashboard. It saves the token where `moin login` does, so you sign in once for both. `esc` quits the sign-in.

## Moving around

Every view fills the screen, from 80 by 24 up. The header says where you are, the footer the keys you need most there, and results and errors: a result for a few seconds, an error in red a little longer.

- **`space`** opens the menu. It lists every action of the place you are in, grouped, each with one key: `space a s` opens the app `shop`, `space R` redeploys the open app. `backspace` goes up a level, `esc` closes it.
- **`space space`** searches, like the leader key of LazyVim or Doom Emacs: type a few letters of an app, a server, a view or an action, like `shop logs` or `redeploy api`, then `enter`. The words may come in any order, the letters it matched show bold, and what you picked last comes first. `ctrl+k` does the same from anywhere, and `/` everywhere but in the logs.
- **`esc`** always goes back one level and closes what is open first. `q` quits on the dashboard and goes back elsewhere; `ctrl+c` quits from anywhere.
- **`?`** shows every key of the place you are in, grouped, and the ones that work everywhere; on a small screen `↑` `↓` scroll them.
- **`↑` `↓`** or **`j` `k`** move, `g` `G` go to the first and the last row, `pgup` `pgdown` a page.
- **The mouse** wheel scrolls what the arrows move, and a click on a tab of an app opens it. Hold `shift` to select text, as in other full-screen apps.

Colour only ever says a status: green live, amber running, red failed, always next to a dot and a word. The TUI reads whether your terminal is dark or light and picks its colours for it. With `NO_COLOR` set, or in a terminal without colour, it draws in plain text and marks the selected row with `›`.

A question like "Stop shop?" waits for `y`; `esc` cancels. A restore cannot be undone, so it asks you to type the app's name first, then `enter`; you can paste it. A line to type into, like a new env var, takes `enter` and `esc`, `ctrl+u` clears it and `ctrl+w` deletes the last word. A switch, a paused check or an env var changes on the screen at once; if the control plane refuses it, the footer says why in red and the tab loads what is true again.

## Views

| View | Shows |
| --- | --- |
| Dashboard | the start: apps live, servers online, backups checked and updates, then your apps with their status, the servers with CPU and memory, the recent activity and what needs you |
| App | one app with seven tabs: deploys, logs, backups, env, domains, uptime and metrics |
| Deploy | the log of one deploy, live while it runs |
| Run | one command in a container of the app and what it writes |
| Servers | every server with its status, system, agent, apps, access and when it was last seen |
| Server | one server's CPU, memory and disk over a range, what its agent set up, its rules and its apps |
| Activity | who did what in your workspace, newest first |
| Web app | the setup pages, to open in the browser |

## Keys

| Key | Where | Does |
| --- | --- | --- |
| `space` | everywhere | the menu of every action here |
| `space space`, `ctrl+k` or `/` | everywhere | searches every app, server, view and action |
| `esc` | everywhere | back one level |
| `?` | everywhere | every key of this place |
| `q` | everywhere | quits on the dashboard, goes back elsewhere |
| `tab` | dashboard | moves between apps, servers and what needs you |
| `enter` | dashboard | opens the selected app, server or issue |
| `s` `a` `w` | dashboard | the servers, the activity, the web app |
| `D` | dashboard, app | deploys the directory `moin` started in to its app, after `y`; `moin deploy` creates a new app |
| `d` `l` `b` `e` `o` `u` `m` | app | deploys, logs, backups, env, domains, uptime, metrics; `tab` and `←` `→` go to the next and the one before |
| `R` | app | redeploys the newest source or image, after `y` |
| `T` | app | restarts the live release with today's env and domains, after `y` |
| `S` | app | stops the app, after `y`; starts a stopped one |
| `P` | app | rolls back to the release before the live one, after `y` |
| `I` | app | deploys an image built in CI: type it, then `enter` |
| `B` | app | deploys a branch, tag or commit of the linked GitHub repo; empty deploys the linked branch |
| `U` | app | updates a catalog app to its newest release, after `y` |
| `N` | app | backs up the app's databases now |
| `X` | app | [runs a command](/docs/guides/exec) in the main service, no shell; start it with `@service` for another one |
| `W` | app, server | opens it in the web app |
| `enter` | deploys | the log of the picked deploy, live while it runs |
| `r` | deploys, deploy | rolls back to the picked deploy, after `y` |
| `/` | logs | filters the lines while you type; `c` clears it |
| `p` | logs | pauses and resumes; the logs load every three seconds |
| `↑` `↓`, `G` | logs, deploy, run | scrolls back; `G` follows the end again |
| `enter` | backups | restores the picked snapshot once you typed the app's name |
| `t` | backups | restores to a point in time: `←` `→` an hour, `H` `L` a day, `↑` `↓` a minute, `tab` the database, `enter`, then the app's name |
| `a` | backups | turns the snapshot before every change on or off |
| `a` | env | sets `KEY=value`; `enter` edits the picked one |
| `x` | env, domains | removes the picked one, after `y` |
| `v` | env | shows or hides the values |
| `E` | env | opens all env vars as one `.env` in `$EDITOR`, and asks before it saves what changed |
| `a` | domains | adds a domain, or `service:domain` for another service of `compose.yml` |
| `c` | domains | makes the picked domain the canonical one |
| `p` | domains | password protection: `user:password`, or removes it after `y` |
| `p` | uptime | pauses or resumes the check of the picked domain |
| `c` | uptime | sets the path it checks, empty for the default |
| `r` | uptime | checks again |
| `[` `]` | metrics, server | the range: 1h, 24h, 7d, 30d |
| `u` | servers, server | updates the agent to the control plane's version, after `y` |
| `enter` | servers, activity | opens the server, or the app or server of the event |
| `enter` or the letter | web app | opens the page in your browser |

## In the web app

These open in the browser from the web app view (`w` on the dashboard, or `space w`), signed in as you: add a server, the catalog, the team, connect GitHub, buckets, alert destinations, the data browser, your settings with API tokens and registry logins, and on moin.run your billing. The CLI, the API and MCP reach most of them too; [Where things work](/docs/reference/surfaces) says which.
