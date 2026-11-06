---
title: "Where things work"
description: "What the web app, the CLI, the terminal UI, the API and MCP can each do"
order: 0
---

moin has five places: the web app, the [CLI](/docs/reference/cli), the [terminal UI](/docs/reference/tui), the [API](/docs/reference/api) and [MCP](/docs/guides/mcp) for AI agents. Everything works in the web, the API, the CLI and MCP. The terminal UI covers the daily work and opens the web app for setup you do once. Where a place lacks something, the table says why.

✓ means it works there end to end.

## Daily work

| Feature | Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- | --- |
| What needs you, what runs | ✓ | ✓ `status` | ✓ the dashboard | ✓ | ✓ |
| Servers, live | ✓ | ✓ `servers` | ✓ | ✓ | ✓ |
| Server metrics, 1h to 30d | ✓ | ✓ `metrics --server` | ✓ | ✓ | ✓ |
| Update an agent | ✓ | ✓ `servers update` | ✓ `u` | ✓ | ✓ |
| Apps, live | ✓ | ✓ `apps` | ✓ | ✓ | ✓ |
| Pin an app | ✓ the star | ✓ `apps pin` | ✓ `*` | ✓ | ✓ `pin_app` |
| Sort the apps | ✓ | ✓ `apps sort` | follows your choice | ✓ | follows your choice |
| Deploy this directory | Your files are on your machine, not in the browser. | ✓ `deploy` | ✓ `D`, for an app that exists | ✓ | An agent has no directory to upload. Use the CLI. |
| Deploy an image from CI | ✓ | ✓ `deploy --image` | ✓ `I` | ✓ | ✓ |
| Deploy a branch, tag or commit | ✓ | ✓ `deploy --ref` | ✓ `B` | ✓ | ✓ |
| Redeploy | ✓ | ✓ | ✓ `R` | ✓ | ✓ |
| Follow a deploy live | ✓ | ✓ | ✓ | ✓ events stream | Polls `get_deploy`: a tool answers once, it does not stream. |
| Deploy history and logs | ✓ | ✓ `deploys` | ✓ | ✓ | ✓ |
| Roll back | ✓ | ✓ `rollback` | ✓ `r`, `P` | ✓ | ✓ |
| Restart, stop, start | ✓ | ✓ | ✓ `T`, `S` | ✓ | ✓ |
| Container logs | ✓ | ✓ `logs` | ✓ live, with a filter | ✓ | ✓ |
| Run a command in a container | ✓ | ✓ `exec` | ✓ `X` | ✓ | ✓ |
| Env vars: see, set, remove | ✓ | ✓ `env` | ✓ | ✓ | ✓ |
| Env vars as one `.env` | ✓ | ✓ `env push`, `env pull` | ✓ in `$EDITOR` | ✓ | ✓ |
| Domains and their DNS | ✓ | ✓ `domains` | ✓ | ✓ | ✓ |
| Add, remove, canonical domain | ✓ | ✓ | ✓ | ✓ | ✓ |
| Password protection | ✓ | ✓ `protect` | ✓ `p` | ✓ | ✓ |
| Uptime and outages | ✓ | ✓ `uptime` | ✓ | ✓ | ✓ |
| Uptime check path, pause | ✓ | ✓ | ✓ | ✓ | ✓ |
| App metrics, 1h to 30d | ✓ | ✓ `metrics` | ✓ | ✓ | ✓ |
| Backups and restore checks | ✓ | ✓ `backups` | ✓ | ✓ | ✓ |
| Back up now | ✓ | ✓ `backup` | ✓ `N` | ✓ | ✓ |
| Restore a snapshot | ✓ | ✓ `restore` | ✓ | ✓ | ✓ |
| Restore to a point in time | ✓ | ✓ `restore --at` | ✓ `t` | ✓ | ✓ |
| Snapshot before every change | ✓ | ✓ `backups auto` | ✓ `a` | ✓ | ✓ |
| Update a catalog app | ✓ | ✓ `update` | ✓ `U` | ✓ | ✓ |
| Activity | ✓ | ✓ `activity` | ✓ | ✓ | ✓ |
| Activity by app, server or member | ✓ | ✓ `--app`, `--server`, `--user` | Daily work wants all of it; filter in the CLI or the web. | ✓ | ✓ |
| Jump to anything | ✓ command palette | Commands take the app by name. | ✓ `space space`, `ctrl+k` | Not a place to jump in. | Not a place to jump in. |

## Setup

The terminal UI opens these in your browser: `w` on the dashboard, or `space w`.

| Feature | Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- | --- |
| Add a server, install command | ✓ | ✓ `servers add` | Setup: opens the web. | ✓ | ✓ |
| Rename, remove a server | ✓ | ✓ | Setup: opens the web. | ✓ | ✓ |
| Tunnel, SSH on Tailscale, firewall | ✓ | ✓ `servers secure` | Shows what the agent set up; `W` changes it in the web. | ✓ | ✓ |
| Server rules | ✓ | ✓ `servers rules` | ✓ on the server | ✓ | ✓ |
| Cloudflare token, Tailscale key | ✓ | ✓ `integrations` | Setup: opens the web. | ✓ | ✓ |
| Agent auto-update | ✓ | ✓ `servers auto-update` | Setup: opens the web. | ✓ | ✓ |
| Uninstall the agent | It runs on the server, not in a browser. | ✓ `agent uninstall`, on the server | It runs on the server. | It runs on the server. | It runs on the server. |
| Create, remove an app | ✓ | ✓ `apps add`, `deploy` | Setup: opens the web. `moin deploy` creates one too. | ✓ | ✓ |
| Catalog: browse and install | ✓ | ✓ `catalog` | A form wants a page: opens the web. | ✓ | ✓ |
| Connect GitHub | ✓ | Shows the link: GitHub needs a browser. | GitHub needs a browser. | Gives the link: GitHub needs a browser. | Gives the link: GitHub needs a browser. |
| Link or unlink a repo | ✓ | ✓ `link`, `unlink` | Setup: opens the web. | ✓ | ✓ |
| Deploy hook for CI | ✓ | ✓ `hook` | Setup: opens the web. | ✓ | ✓ |
| Registry logins | ✓ | ✓ `registry` | Setup: opens the web. | ✓ | ✓ |
| Instant address: provider, open | ✓ | ✓ `instant` | Shows it; change it in the web. | ✓ | ✓ |
| Buckets | ✓ | ✓ `buckets` | Setup: opens the web. | ✓ | ✓ |
| Move an app to another bucket | ✓ | ✓ `buckets use` | Setup: opens the web. | ✓ | ✓ |
| Restore from an older bucket | ✓ | ✓ `restore --bucket` | ✓ the snapshot knows its bucket | ✓ | ✓ |
| Data: tables, rows, schema | ✓ | ✓ `db` | A grid of rows wants a page: opens the web. | ✓ | ✓ `list_tables`, rows through SQL |
| SQL, read and write | ✓ | ✓ `sql` | Opens the web. | ✓ | ✓ |
| Alert destinations | ✓ | ✓ `alerts add` | Setup: opens the web. | ✓ | ✓ |
| Email alerts on or off | ✓ | ✓ `alerts on`, `alerts off` | Setup: opens the web. | ✓ | ✓ |
| Members and invitations | ✓ | ✓ `team` | Setup: opens the web. | ✓ | ✓ |
| Rename the workspace | ✓ | ✓ `team rename` | Setup: opens the web. | ✓ | ✓ |
| Update a self-hosted control plane | ✓ | ✓ `update` | Opens the web. | ✓ | ✓ |

## Account, team and money

These stay in the web on purpose: they move money or decide who is in charge, so they want your password, not a token.

| Feature | Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- | --- |
| Sign up, sign in, passwords, email | ✓ | `login` signs this machine in; the rest is the web. | Signs in with the browser; the rest is the web. | ✓ with a session | Agents use a token, never your password. |
| Sessions | ✓ | Web only: a token can't see your sessions. | Web only. | ✓ with a session | Web only. |
| API tokens | ✓ | Web only: a token can't make tokens. | Web only: a token can't make tokens. | ✓ with a session | Web only: a token can't make tokens. |
| Display name, avatar | ✓ | Set it in the web. | Set it in the web. | ✓ | Set it in the web. |
| Change roles, remove members, leave, delete the workspace | ✓ | Web only: who is in charge is decided with a password. | Web only. | ✓ with a session | Web only. |
| Create a workspace, accept an invitation | ✓ | Web only: it starts with a signed-in browser. | Web only. | ✓ with a session | Web only. |
| Plan and trial | ✓ | Web only. | Web only. | ✓ | ✓ `get_billing` |
| Checkout, card, invoices | ✓ | Your card belongs to Stripe, not your terminal. | Your card belongs to Stripe, not your terminal. | ✓ with a session | Agents never move money. |
| Delete your account | ✓ | Web only, with your password. | Web only. | ✓ with a session | Agents never delete accounts. |

"With a session" means the endpoint answers a signed-in browser and refuses personal API tokens; the [API reference](/docs/reference/api) marks each one.
