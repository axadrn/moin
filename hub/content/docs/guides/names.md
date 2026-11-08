---
title: "Names and links"
description: "Rename apps, servers, buckets and the workspace"
group: "Apps"
order: 8
---

Everything has a name you give it: the workspace, every app, server and bucket. A link is made of those names.

```text
/app/acme/apps/my-app
     └──┘      └────┘
   workspace    app
```

Rename `my-app` to `blog` and its link becomes `/app/acme/apps/blog`. Every link inside moin follows by itself. An old bookmark shows "not found".

## The ID

Everything also has an ID of 32 characters that never changes. The API (`id`), the CLI, the TUI and MCP show it. Wherever you name a thing, its name or its ID works:

```text
moin deploy my-app
moin deploy 6f1c2a3b…
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

A renamed workspace links with its new name too. Old bookmarks show "not found".
