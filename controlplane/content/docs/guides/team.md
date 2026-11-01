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

## Invite someone and manage the team

| Action | Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- | --- |
| See members and invitations | **Team** | `ops team` | the `team` tab | `GET /api/v1/workspaces/{slug}` | `get_workspace` |
| Invite | **Team**, then **Send invite** | `ops team invite ada@example.com [--admin]` | team tab, `a` | `POST /api/v1/workspaces/{slug}/invitations` | `invite_member` |
| Send an invitation again | **Resend** under **Pending invitations** | – | team tab, the invitation, `enter` | `POST …/invitations/{id}/resend` | – |
| Revoke an invitation | **Revoke** under **Pending invitations** | – | team tab, the invitation, `x`, then `y` | `DELETE …/invitations/{id}` | – |
| Rename the workspace | **Team**, **General**, **Save** | – | team tab, `n` | `PATCH /api/v1/workspaces/{slug}/name` | `rename_workspace` |
| Change a role | **Role** on a member, **Save** | web only | web only | session only | web only |
| Remove a member | **Remove** on a member | web only | web only | session only | web only |
| Leave a workspace | **Leave workspace** | web only | web only | session only | web only |
| Delete a workspace | **Delete workspace** | web only | web only | session only | web only |

The invitation is an email that works for seven days. When someone is removed, their tokens stop reaching the workspace right away.

Changing roles, removing people, leaving and deleting a workspace work only in the web, signed in: an API token, and so the CLI, the terminal UI and agents, cannot do them. A leaked token can never lock your team out or delete its work.

## Tokens and workspaces

A personal API token belongs to one workspace: you pick it when you create the token under **Settings**. The CLI, the terminal UI, the GitHub Action and agents work in that workspace. For another workspace, log in with one of its tokens:

```bash
ops login --token pat_…
```

## Activity

Every change goes into the activity of the workspace: who did it, when, and how it came in.

| Web | CLI | TUI | API | MCP |
| --- | --- | --- | --- | --- |
| **Activity**, or `g` then `y` | `ops activity [--limit 30]` | the `activity` tab | `GET /api/v1/activity` | `list_activity` |

It records deploys, redeploys, rollbacks, restores, snapshots, SQL writes, row edits, env vars, domains, passwords, registry logins, the bucket, GitHub, servers, their security and every member change. **Via** says how it came in: the web, the CLI (any API token), an agent over MCP, a deploy hook or a GitHub push.

The activity never holds your data: env vars show their names, never their values, a SQL write shows what it did and how many rows it changed, such as `ran UPDATE on orders in shop, 3 rows changed`, never its SQL or a value, and a row edit names the table, row and column, never the value.
