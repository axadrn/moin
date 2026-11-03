---
title: "Team and activity"
description: "Work on the same servers with your team and see who changed what"
order: 14
---

**The short version:** invite your team, everyone deploys to the same servers, and the activity shows who did what. Teams are free.

## Your workspace

Everything you add to ops lives in a workspace: servers, apps, the bucket, registry logins, the Cloudflare token, the Tailscale key and the GitHub App. Everyone in the workspace sees and works on all of it. The plan belongs to the workspace too, so one subscription covers the whole team.

You name your first workspace when you sign up. Rename it later under [Rename the workspace](#rename-the-workspace).

## Create a workspace

Makes one more workspace, for example one per client. On hosted ops each workspace has its own plan.

```surfaces
Web: the workspace switcher at the top of the sidebar, **New workspace**
API: POST /api/v1/workspaces
```

The new workspace starts empty. Your API tokens stay with their own workspace: [make a token](/docs/guides/account#create-an-api-token) in the new one for the CLI.

## Roles

| | Member | Admin | Owner |
| --- | --- | --- | --- |
| Deploy, roll back, edit data, restore, servers, domains, env | ✓ | ✓ | ✓ |
| See the activity | ✓ | ✓ | ✓ |
| Invite people, rename the workspace | | ✓ | ✓ |
| Make someone a member or an admin, remove members and admins | | ✓ | ✓ |
| Make someone an owner, remove an owner, billing, delete the workspace | | | ✓ |

Everyone can leave a workspace. A workspace always keeps one owner: the last owner hands the role to someone else first, or deletes the workspace.

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
CLI: ops team invite <email>
TUI: on the team tab, `a`
API: POST /api/v1/workspaces/{slug}/invitations
MCP: invite_member
```

`--admin` in the CLI invites them as an admin. They sign up with the address you invited, then land in your workspace. What each role may do is in [Roles](#roles).

## Send an invitation again

Sends the email again, for example when it landed in spam.

```surfaces
Web: **Team**, **Pending invitations**, **Resend**
CLI: ops team resend <email>
TUI: on the team tab, pick the invitation, then `enter`
API: POST /api/v1/workspaces/{slug}/invitations/{id}/resend
MCP: resend_invitation
```

Only the new link works.

## Revoke an invitation

Takes an invitation back before someone accepts it.

```surfaces
Web: **Team**, **Pending invitations**, **Revoke**
CLI: ops team revoke <email>
TUI: on the team tab, pick the invitation, `x`, then `y`
API: DELETE /api/v1/workspaces/{slug}/invitations/{id}
MCP: revoke_invitation
```

## Rename the workspace

Changes the name everyone in the workspace sees.

```surfaces
Web: **Team**, **Workspace name**, **Save**
CLI: ops team rename "<workspace-name>"
TUI: on the team tab, `cw` or `:rename`
API: PATCH /api/v1/workspaces/{slug}/name
MCP: rename_workspace
```

## Change a role

Makes a member an admin, or the other way round. Only an owner makes someone an owner or changes an owner's role.

```surfaces
Web: **Team**, **Members**, the role next to the person, then **Save**
API: PATCH /api/v1/workspaces/{slug}/members/{id}/role
```

## Remove a member

Takes someone out of the workspace. Their API tokens for it stop working right away.

```surfaces
Web: **Team**, **Members**, **Remove** next to the person
API: DELETE /api/v1/workspaces/{slug}/members/{id}
```

## Leave or delete a workspace

**Leave** takes you out of the workspace. Its servers and apps stay for the others.

**Delete workspace** removes the workspace with its members, invitations, activity, servers and apps from ops. Your apps keep running on your servers, and your bucket keeps its backups. Only an owner deletes a workspace, and you type its name first. It cannot be undone. A paid plan must be cancelled and run out first.

```surfaces
Web: **Team**, **Leave**, then **Leave workspace**
Web: **Team**, **Delete workspace**, then type its name
API: POST /api/v1/workspaces/{slug}/leave
API: DELETE /api/v1/workspaces/{slug}
```

The last owner cannot leave: [change a role](#change-a-role) to make someone else an owner first.

## Why only the web

Changing roles, removing people, leaving and deleting a workspace need you signed in on the web. An API token, and so the CLI, the TUI and agents, cannot do them. A leaked token can never lock your team out or delete its work.

## Tokens and workspaces

A personal API token belongs to one workspace: you pick it when you create the token under **Settings**. The CLI, the terminal UI, the GitHub Action and agents work in that workspace. For another workspace, log in with one of its tokens:

```bash
ops login --token pat_…
```

## Activity

Every change goes into the activity of the workspace: who did it, to the second, how it came in and what changed. Filter it by app, server or member and page back as far as you like. The [activity log](/docs/guides/activity) shows what it records and what it never records.
