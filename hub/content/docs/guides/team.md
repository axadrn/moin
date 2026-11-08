---
title: "Team"
description: "Invite your team to the same servers and apps, with roles"
group: "Team"
order: 1
---

Your team is the people who work with you. Invite them, and everyone works on the same apps and servers. Teams are free.

You all share one workspace, the space where everything lives. One plan covers the whole team.

```surfaces
Web: **Settings**, **Team**
CLI: moin team
TUI: opens the web: `w`, then `t`
API: GET /api/v1/workspaces/{slug}
MCP: get_workspace
```

## Roles

Each role can do everything inside it.

```text
┌ owner ──────────────────────────┐
│ also pays, makes owners,        │
│ deletes the workspace           │
│ ┌ admin ──────────────────────┐ │
│ │ also invites, renames,      │ │
│ │ changes roles               │ │
│ │ ┌ member ─────────────────┐ │ │
│ │ │ deploys, rolls back,    │ │ │
│ │ │ restores, edits data,   │ │ │
│ │ │ sees the activity       │ │ │
│ │ └─────────────────────────┘ │ │
│ └─────────────────────────────┘ │
└─────────────────────────────────┘
```

| | Member | Admin | Owner |
| --- | --- | --- | --- |
| Deploy, roll back, edit data, restore, servers, domains, env | ✓ | ✓ | ✓ |
| See the activity | ✓ | ✓ | ✓ |
| Invite people, rename the workspace | | ✓ | ✓ |
| Make someone a member or an admin, remove members and admins | | ✓ | ✓ |
| Make someone an owner, remove an owner, billing, delete the workspace | | | ✓ |

Everyone can leave a workspace. A workspace always keeps one owner.

## Invite someone

moin sends an email to join the workspace. It is good for seven days. They sign up with that address and land in your workspace.

```surfaces
Web: **Settings**, **Team**, **Invite a teammate**, **Send invite**
CLI: moin team invite <email>; --admin invites an admin
TUI: opens the web: `w`, then `t`
API: POST /api/v1/workspaces/{slug}/invitations
MCP: invite_member
```

## Send an invitation again

Only the new link works.

```surfaces
Web: **Settings**, **Team**, **Invitations**, **Resend**
CLI: moin team resend <email>
TUI: opens the web: `w`, then `t`
API: POST /api/v1/workspaces/{slug}/invitations/{id}/resend
MCP: resend_invitation
```

## Revoke an invitation

```surfaces
Web: **Settings**, **Team**, **Invitations**, **Revoke**
CLI: moin team revoke <email>
TUI: opens the web: `w`, then `t`
API: DELETE /api/v1/workspaces/{slug}/invitations/{id}
MCP: revoke_invitation
```

## Rename the workspace

Its old links keep working ([Names and links](/docs/guides/names)).

```surfaces
Web: **Settings**, **Team**, **Workspace name**, **Save**
CLI: moin team rename "<workspace-name>"
TUI: opens the web: `w`, then `t`
API: PATCH /api/v1/workspaces/{slug}/name
MCP: rename_workspace
```

## Change a role

Only an owner makes someone an owner or changes an owner's role.

```surfaces
Web: **Settings**, **Team**, the role next to the person; it saves at once
API: PATCH /api/v1/workspaces/{slug}/members/{id}/role
```

## Remove someone

Their API tokens for the workspace stop working at once.

```surfaces
Web: **Settings**, **Team**, **Remove** next to the person
API: DELETE /api/v1/workspaces/{slug}/members/{id}
```

## Create a workspace

One more workspace, for example one per client. On hosted moin each has its own plan. Your API tokens stay with their own workspace: [make a token](/docs/guides/account#create-an-api-token) in the new one.

```surfaces
Web: **Settings**, **Workspaces**, **New workspace**
API: POST /api/v1/workspaces
```

## Leave or delete a workspace

**Leave workspace** takes you out; servers and apps stay for the others. The last owner cannot leave: [change a role](#change-a-role) first.

**Delete workspace** removes it from moin, with its team, activity, servers and apps. Your apps keep running on your servers, and your bucket keeps its backups. Only an owner can do this, after typing its name. It cannot be undone. A paid plan must be cancelled and run out first.

```surfaces
Web: **Settings**, **Team**, **Leave workspace**
Web: **Settings**, **Team**, **Delete workspace**, then type its name
API: POST /api/v1/workspaces/{slug}/leave
API: DELETE /api/v1/workspaces/{slug}
```

## Good to know

- Changing roles, removing people, leaving and deleting a workspace need you signed in on the web. A leaked token never locks your team out.
- A personal API token belongs to one workspace. For another one, log in with one of its tokens: `moin login --token pat_…`.
