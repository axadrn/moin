---
title: "Activity log"
description: "Who changed what, to the second, and what ops never writes down"
order: 15
---

**Know who changed what, to the second.** Every deploy, every env change, every click in the data browser. Your secrets and your data never.

## What it looks like

| When (UTC) | Who | Via | What |
| --- | --- | --- | --- |
| 2026-11-02 14:03:07 | ada@example.com | CLI, token laptop | deployed shop v42 from main at 3f2a9c1 |
| 2026-11-02 14:03:51 | ops | ops | made v42 of shop live after 44s |
| 2026-11-02 14:10:12 | ben@example.com | Web | set STRIPE_KEY in the env of shop |
| 2026-11-02 14:12:30 | ben@example.com | Web | edited status of row 1182 in orders of shop |
| 2026-11-02 14:20:02 | GitHub | GitHub | deployed blog v7 from main at 9be41d0 |
| 2026-11-02 14:25:44 | ada@example.com | MCP, token claude | ran UPDATE on orders in shop, 3 rows changed |
| 2026-11-03 03:00:18 | ops | ops | restored app.db of shop in a restore drill and checked it |

That is the real format. Each row holds the exact second, who did it, how it came in and what changed.

## What we log

- **When.** The exact time, stored in UTC to the fraction of a second. The web shows UTC with seconds, the CLI and the TUI your local time with its zone, the API and MCP RFC 3339.
- **Who.** The member, by email. The API token, by name. A deploy hook. A GitHub push, with its branch and commit. Or ops itself, for a deploy that ended, a server that connected or a restore drill.
- **How.** The web, the CLI, the TUI, the API, an agent over MCP, a deploy hook or GitHub.
- **Where from.** The IP address and browser or program of the request. Only admins and owners see them.
- **What.** The action and what it touched: the app, the server, the member, the token.
- **Before and after.** Names, on and off, domains, branches, roles, counts, durations, versions, release numbers, exit codes and error messages.

It covers deploys and how they ended, redeploys, restarts, starts, stops, rollbacks, env vars, domains, passwords in front of apps, deploy hooks, GitHub links, registry logins, the bucket, snapshots, restores, restore drills, SQL writes, row edits, servers, their security, agent updates, API tokens, plan changes and every change of the team.

## What we never log

- Env var values. Only their names.
- Secrets, tokens and passwords.
- Your SQL. A SQL write keeps the kind of statement, the tables and the row count, such as `UPDATE on orders, 3 rows changed`.
- Rows and cell values. A row edit keeps the table, the row and the column, never the value.
- File contents and the log lines of your apps.

ops keeps none of your data. Your code and your data live on your server and in your bucket. The activity log is no exception.

## See the activity

Lists who changed what, newest first.

```surfaces
Web: **Activity** in the sidebar
CLI: ops activity
TUI: the **activity** tab
API: GET /api/v1/activity
MCP: list_activity
```

On the web, `g` then `y` opens it too.

## Find one change

Narrow it to one app, one server or one member. A server shows its own changes and the ones of its apps.

```surfaces
Web: **Activity**, pick an app, a server or a member, then **Filter**
CLI: ops activity --app <app-name> --server <server-name> --user <email>
TUI: on the activity tab, `f`; `c` clears it
API: GET /api/v1/activity?app=<app-name>&server=<server-name>&user=<email>
MCP: list_activity
```

## Go further back

Everything stays. Page back as far as you like.

```surfaces
Web: **Activity**, **Show older** at the end
CLI: ops activity --cursor <cursor>
TUI: on the activity tab, `m`
API: GET /api/v1/activity?cursor=<next_cursor>
MCP: list_activity
```

The CLI prints the next `--cursor` in its hint at the end. In the API and MCP, send `next_cursor` back as `cursor`, and filter with `app`, `server` and `user`. `ops activity --limit 200` shows up to 200 at once.

## Good to know

- Every member can see the activity. Only admins and owners see IP addresses and browsers.
- When someone deletes their account, their email stays on their old changes.
- Deleting a workspace deletes its activity too.
- Who may do what is in [Roles](/docs/guides/team#roles).
