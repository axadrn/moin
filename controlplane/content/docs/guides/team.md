---
title: "Team and activity"
description: "Work on the same servers with your team and see who changed what"
order: 14
---

**The short version:** invite your team, everyone deploys to the same servers, and the activity shows who did what. Teams are free.

## Your workspace

Everything you add to ops lives in a workspace: servers, apps, the bucket, registry logins, the Cloudflare token, the Tailscale key and the GitHub App. Everyone in the workspace sees and works on all of it. The plan belongs to the workspace too, so one subscription covers the whole team.

You get a workspace when you sign up. Make more from the workspace switcher at the top of the sidebar, for example one per client.

## Roles

| | Member | Admin | Owner |
| --- | --- | --- | --- |
| Deploy, roll back, edit data, restore, servers, domains, env | ✓ | ✓ | ✓ |
| See the activity | ✓ | ✓ | ✓ |
| Invite people, rename the workspace | | ✓ | ✓ |
| Change roles, billing, delete the workspace | | | ✓ |

## Invite someone

| Web | CLI | MCP |
| --- | --- | --- |
| **Team**, then **Send invite** | `ops team invite ada@example.com [--admin]` | `invite_member` |

The invitation is an email that works for seven days. `ops team` lists the members and the invitations still open. To take someone out, remove them on the **Team** page; their tokens stop reaching the workspace right away.

## Tokens and workspaces

A personal API token belongs to one workspace: you pick it when you create the token under **Settings**. The CLI, the terminal UI, the GitHub Action and agents work in that workspace. For another workspace, log in with one of its tokens:

```bash
ops login --token pat_…
```

## Activity

Every change goes into the activity of the workspace: who did it, when, and how it came in.

| Web | CLI | Terminal UI | API | MCP |
| --- | --- | --- | --- | --- |
| **Activity**, or `g` then `y` | `ops activity [--limit 30]` | the `activity` tab | `GET /api/v1/activity` | `list_activity` |

It records deploys, redeploys, rollbacks, restores, snapshots, SQL writes, row edits, env vars, domains, passwords, registry logins, the bucket, GitHub, servers, their security and every member change. **Via** says how it came in: the web, the CLI (any API token), an agent over MCP, a deploy hook or a GitHub push.

The activity never holds your data: env vars show their names, never their values, a SQL write shows how many rows it changed, never its SQL, and a row edit names the table, row and column, never the value.
