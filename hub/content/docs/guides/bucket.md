---
title: "Bucket"
description: "Your backups in your own S3-compatible bucket. Never with us."
group: "Servers and data"
order: 5
---

Your bucket is your own box on the internet. Your backups go there. Only there. Never to us. Its secret key lives on your server, not in the hub. The server signs the short-lived download links too.

Any S3-compatible bucket works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner Object Storage, Exoscale or MinIO. One bucket serves every app.

## Add it

Make a bucket at your provider, and a key that may read, write and delete in it. The first bucket is the workspace's default.

```surfaces
Web: **Settings**, **Bucket**, **Add bucket**
CLI: moin buckets add default \
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --bucket <bucket-name> \
  --access-key <access-key> \
  --server web-1
TUI: opens the web: `w`, then `k`
API: POST /api/v1/buckets
MCP: add_bucket
```

| Field | CLI | What to type |
| --- | --- | --- |
| Name | first argument | your name for it, like `default`: lowercase letters, digits and dashes |
| Endpoint | `--endpoint` | starts with `https://`; for Cloudflare R2 `https://<account-id>.r2.cloudflarestorage.com`. Empty for AWS S3 |
| Bucket | `--bucket` | the bucket's name |
| Folder | `--path` | usually empty; set it when the bucket holds other files too |
| Access key, Secret key | `--access-key` | the two parts of the key; the CLI asks for the secret key on stdin |
| Region | `--region` | `auto` for Cloudflare R2, the bucket's region for AWS S3, else what your provider says |
| Server | `--server` | the server that tests the bucket and keeps the secret key; with one server, moin picks it |

The server writes and deletes a test file first, so a wrong key fails at once. Then it keeps the secret key in `/var/lib/moin/server.json`, readable by root only. The hub keeps the rest, never the secret key. The server must be online.

## Give another server the key

Each server keeps its own copy of the key. A server added later has none, and its backups to this bucket wait. The bucket's list names it: `web-2 has no key for this bucket`. Enter the key for that server:

```surfaces
Web: **Settings**, **Bucket**: the secret key in the server's row, then **Save**
CLI: moin buckets key default --server web-2
TUI: opens the web: `w`, then `k`
API: PUT /api/v1/buckets/{name}/key
MCP: set_bucket_key
```

The server tests the bucket with it first, like on add. A server you rebuilt from scratch needs the key again too.

## What needs it

| | Needs the bucket |
| --- | --- |
| Backups, restores and **Download a copy** | yes |
| Deploys from [GitHub](/docs/guides/github), also `moin deploy` | no: the server downloads the commit itself |
| Image deploys from [CI](/docs/guides/ci) | no: the server pulls the image |
| [Catalog](/docs/guides/catalog) apps | no: the daemon brings their compose files |

## See your buckets

```surfaces
Web: **Settings**, **Bucket**
CLI: moin buckets
TUI: opens the web: `w`, then `k`
API: GET /api/v1/buckets
MCP: list_buckets
```

Each bucket says which servers keep its key, and which do not.

## More than one bucket

Add another bucket the same way, with another name. Every app without a bucket of its own uses the default.

### Choose the bucket of an app

Moves the backups of one app to another bucket. Its copies start again there. What is in the old bucket stays.

```surfaces
Web: the app, **Data**, **Use another bucket**, then **Move**
CLI: moin buckets use client-a [app]
CLI: moin buckets use --default [app]
TUI: on the app, `W`, then **Data** in the web
API: PUT /api/v1/apps/{app}/bucket
MCP: set_app_bucket
```

### Change a bucket

Renames it, points it at another place, gives it new keys or makes it the default. A new place or a new key is tested by a server, the one you name with `--server`. An empty secret key keeps the one the server has. A new access key or secret key goes to that server only: enter it again for the others.

```surfaces
Web: **Settings**, **Bucket**, **Change** on the bucket, then **Save bucket**; **Make default**
CLI: moin buckets edit client-a --name client-b
CLI: moin buckets edit client-a --access-key <access-key>
CLI: moin buckets default client-a
TUI: opens the web: `w`, then `k`
API: PUT /api/v1/buckets/{name}
MCP: edit_bucket
MCP: set_default_bucket
```

### Remove a bucket

Removes it from moin and stops its backups. What is in it stays there.

```surfaces
Web: **Settings**, **Bucket**, **Remove** on the bucket, then type its name
CLI: moin buckets rm client-a
TUI: opens the web: `w`, then `k`
API: DELETE /api/v1/buckets/{name}
MCP: remove_bucket
```

moin refuses a bucket that apps still use or moved away from, and names those apps. Move them first. You type the bucket's name, and the hub checks it: a script passes it as `--confirm <name>`, the API as `?confirm=<name>`, an AI agent as `confirm`.

## Good to know

- The hub never talks to your bucket. Your servers do.
- Two workspaces can share one bucket. Each gets its own folder.
- The `backups-to` [rule](/docs/guides/security#rules) of a server allows one bucket only.
- moin never deletes anything in a bucket you changed or removed. [How long things stay](/docs/concepts/backups#how-long-things-stay).
