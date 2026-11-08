---
title: "Names and links"
description: "Rename apps, servers, buckets and the workspace; links keep working"
group: "Apps"
order: 8
---

Everything has a name you give it and an ID that never changes: the workspace, every app, server and bucket. A link carries both. The name is for you. The ID is for moin.

```text
/app/acme-0f8e…/apps/my-app-6f1c…
     └──┘ └───┘      └────┘ └───┘
     name  ID         name   ID
```

Only the ID counts. Rename `my-app` to `blog` and every old link still opens it.

## The ID

The ID is 32 characters, the same in links, the API (`id`), the CLI, the TUI and MCP. Wherever you name a thing, its name, its ID or a whole link part works:

```text
moin deploy my-app
moin deploy 6f1c2a3b…
moin deploy my-app-6f1c2a3b…
```

## Rename an app

The rules of a new name apply: lowercase letters, digits and dashes, unique in the workspace.

```surfaces
Web: open the app, **Settings**, **Name**, **Save**
CLI: moin apps rename my-app blog
TUI: opens the web: on the app, `W`, then **Settings**
API: PATCH /api/v1/apps/{app}
MCP: rename_app
```

Nothing is lost:

- **Data.** Its volumes keep a fixed name on the server, so the new name finds the same data.
- **Backups.** Its copies stay where they are, and restores find them.
- **Instant address.** It stays the same.
- **Containers.** They take the new name on the next restart or deploy. Until then the app runs as before.

Set `x-moin.app: blog` after the rename. Otherwise `moin deploy` in a folder called `my-app` offers to make a new app.

## Rename the rest

| What | Web | CLI | MCP |
| --- | --- | --- | --- |
| A server | **Servers**, open it, **Name** | `moin servers rename web-1 web-2` | `rename_server` |
| A bucket | **Settings**, **Bucket**, **Change** | `moin buckets edit default --name archive` | `edit_bucket` |
| The workspace | **Settings**, **Team**, **Workspace name** | `moin team rename "Acme Inc"` | `rename_workspace` |

A renamed workspace links with its new name too. The old links keep working.
