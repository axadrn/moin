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

## See the team

Lists the members of the workspace and the invitations that wait for an answer.

```surfaces
Web: **Team**
CLI: ops team
TUI: the **team** tab
API: GET /api/v1/workspaces/{slug}
MCP: get_workspace
```

## Invite someone

Sends an email that lets someone join the workspace. The invitation works for seven days.

```surfaces
Web: **Team**, **Invite a teammate**, **Send invite**
CLI: ops team invite ada@example.com
TUI: on the team tab, `a`
API: POST /api/v1/workspaces/{slug}/invitations
MCP: invite_member
```

`--admin` in the CLI invites them as an admin.

## Send an invitation again

Sends the email again, for example when it landed in spam.

```surfaces
Web: **Team**, **Pending invitations**, **Resend**
CLI: ops team resend ada@example.com
TUI: on the team tab, pick the invitation, then `enter`
API: POST /api/v1/workspaces/{slug}/invitations/{id}/resend
MCP: resend_invitation
```

Only the new link works.

## Revoke an invitation

Takes an invitation back before someone accepts it.

```surfaces
Web: **Team**, **Pending invitations**, **Revoke**
CLI: ops team revoke ada@example.com
TUI: on the team tab, pick the invitation, `x`, then `y`
API: DELETE /api/v1/workspaces/{slug}/invitations/{id}
MCP: revoke_invitation
```

## Rename the workspace

Changes the name everyone in the workspace sees.

```surfaces
Web: **Team**, **Workspace name**, **Save**
CLI: ops team rename "Acme Inc"
TUI: on the team tab, `n`
API: PATCH /api/v1/workspaces/{slug}/name
MCP: rename_workspace
```

## Change roles, remove people, leave or delete

These four work only on the web, signed in:

- **Change a role:** **Team**, **Role** on a member, then **Save**.
- **Remove a member:** **Team**, **Remove** on a member. Their tokens stop reaching the workspace right away.
- **Leave a workspace:** **Team**, **Leave workspace**.
- **Delete a workspace:** **Team**, **Delete workspace**, then type its name. It cannot be undone.

An API token, and so the CLI, the TUI and agents, cannot do them. A leaked token can never lock your team out or delete its work.

## Tokens and workspaces

A personal API token belongs to one workspace: you pick it when you create the token under **Settings**. The CLI, the terminal UI, the GitHub Action and agents work in that workspace. For another workspace, log in with one of its tokens:

```bash
ops login --token pat_…
```

## Activity

Every change goes into the activity of the workspace: who did it, to the second, how it came in and what changed. Filter it by app, server or member and page back as far as you like. The [activity log](/docs/guides/activity) shows what it records and what it never records.
