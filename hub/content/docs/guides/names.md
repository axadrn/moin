---
title: "Names and links"
description: "Rename apps, servers, buckets and the workspace; links keep working"
group: "Apps"
order: 8
---

Everything in moin has a name you give it and an ID that never changes: the workspace, every app, server and bucket. A link carries both, like Notion's: the name for you, the ID for moin.

```text
/app/acme-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b/apps/my-app-6f1c2a3b4d5e4f608a7b9c0d1e2f3a4b
     └─ name ─┘└────────── ID ──────────┘      └ name ┘└────────── ID ──────────┘
```

Only the ID counts. Rename `my-app` to `blog` and every old link still opens it, bookmarks and alert emails too; a link with a wrong name opens it as well. moin always links with the name it has now.

## The ID

The ID is 32 characters, the same everywhere: in links, in the API (`id`), the CLI, the TUI and MCP. Wherever you name a thing, its name, its ID or a whole link part works.

```text
moin deploy my-app
moin deploy 6f1c2a3b4d5e4f608a7b9c0d1e2f3a4b
moin deploy my-app-6f1c2a3b4d5e4f608a7b9c0d1e2f3a4b
```

## Rename an app

Same rules as a new name: lowercase letters, digits and dashes, unique in the workspace.

```surfaces
Web: open the app, **Settings**, **Name**, **Save**
CLI: moin apps rename my-app blog
TUI: opens the web: on the app, `W`, then **Settings**
API: PATCH /api/v1/apps/{app}
MCP: rename_app
```

Nothing is lost:

- **Data.** The app's volumes keep a fixed name on its server, `moin-<app ID>-<volume>`, so the new name finds the same data. An app from before renames keeps the names its volumes had.
- **Backups.** Its copies stay where they are in your bucket, and restores find them.
- **Instant address.** It stays the same, so nothing that points at it breaks.
- **Containers.** They take the new name on the next restart or deploy: `docker ps` shows `blog-web-1`. moin stops those of the old name first, so nothing runs twice. Until then the app runs as before, and its page counts the rename as a change that waits.

In the app's repo, `moin deploy` finds the app by `x-moin.app` in `compose.yml`, else by the name of the directory. Set `x-moin.app: blog` after the rename; else `moin deploy` in a directory called `my-app` creates a new app of that name.

## Rename the rest

| What | Web | CLI | MCP |
| --- | --- | --- | --- |
| A server | **Servers**, open it, **Name** | `moin servers rename web-1 web-2` | `rename_server` |
| A bucket | **Settings**, **Bucket**, **Change** | `moin buckets edit default --name archive` | `edit_bucket` |
| The workspace | **Settings**, **Team**, **Workspace name** | `moin team rename "Acme Inc"` | `rename_workspace` |

A renamed workspace links with its new name too: `/app/acme-…` becomes `/app/globex-…`, and the old links keep working.
