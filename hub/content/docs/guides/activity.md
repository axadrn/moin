---
title: "Activity"
description: "Who changed what, to the second, and what moin never writes down"
group: "Team"
order: 2
---

The activity is moin's notebook of who did what, and when. Every change in a workspace, to the second. Your secrets and your data never go in.

```surfaces
Web: **Settings**, **Activity**, or `g` then `y`
CLI: moin activity
TUI: `a` on the overview, or `activity` in the palette (`space`)
API: GET /api/v1/activity
MCP: list_activity
```

| When (UTC) | Who | Via | What |
| --- | --- | --- | --- |
| 2026-11-02 14:03:07 | Ada Lovelace | CLI, token ci | deployed my-app v42 from main at 3f2a9c1 |
| 2026-11-02 14:03:51 | moin | moin | made v42 of my-app live after 44s |
| 2026-11-02 14:10:12 | Ben Okafor | Web | set STRIPE_KEY in the env of my-app |
| 2026-11-02 14:12:30 | Ben Okafor | Web | edited email of row 1182 in users of my-app |
| 2026-11-02 14:25:44 | Ada Lovelace | MCP, token claude | ran UPDATE on users in my-app, 3 rows changed |
| 2026-11-02 16:40:02 | Ada Lovelace | Web | set the backups of my-app to every 15 minutes, kept 30 days, every change on |
| 2026-11-03 03:00:18 | moin | moin | checked that the newest copy of app.db of my-app restores |

## Find one change

Narrow it to an app, a server or a person. A server shows its own changes and those of its apps.

```surfaces
Web: **Settings**, **Activity**, pick an app, a server or a person; the list follows
CLI: moin activity --app <app-name> --server <server-name> --user <email>
TUI: not here: it shows all of it; filter in the CLI or the web
API: GET /api/v1/activity?app=<app-name>&server=<server-name>&user=<email>
MCP: list_activity
```

## Go further back

Everything stays. Page back as far as you like.

```surfaces
Web: **Settings**, **Activity**, **Show older** at the end
CLI: moin activity --cursor <cursor>
TUI: scroll down: older events load at the end
API: GET /api/v1/activity?cursor=<next_cursor>
MCP: list_activity
```

The CLI prints the next `--cursor`. In the API and MCP, send `next_cursor` back as `cursor`. `moin activity --limit 200` shows up to 200 at once.

## What it records

- **When**: the exact time, stored in UTC.
- **Who**: the person, the API token, a deploy hook, a GitHub push, or moin itself.
- **How**: the web, the CLI, the TUI, the API, MCP, a deploy hook or GitHub.
- **Where from**: the IP address and browser. Only admins and owners see them.
- **What**: the action, what it touched, and the settings before and after.

It covers every change: deploys, restarts, env vars, domains, buckets, backups, restores, daily checks, SQL writes, row edits, servers, alerts, API tokens, plan changes and the team.

## What it never records

- Env var values: only their names.
- Secrets, tokens and passwords.
- Your SQL: a write keeps the kind of statement, the tables and the row count, such as `UPDATE on users, 3 rows changed`.
- Rows and cell values: a row edit keeps the table, the row and the column.
- File contents and the log lines of your apps.

## Good to know

- Only admins and owners see emails, IP addresses and browsers.
- Filter by a person with their email in the CLI, the API and MCP.
- Deleting a workspace deletes its activity.
- Who may do what: [Roles](/docs/guides/team#roles).
