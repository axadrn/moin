---
title: "Bucket"
description: "Your code and your backups in your own S3-compatible bucket, never with us"
group: "Servers and data"
order: 5
---

Your bucket is your own box on the internet. Your backups and uploads go there, and only there. Never to us. moin only signs short-lived links into it.

Any S3-compatible bucket works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner Object Storage, Exoscale or MinIO. One bucket serves every app.

## Add it

Make a bucket at your provider, and a key that may read, write and delete in it. The first bucket is the workspace's default.

```surfaces
Web: **Settings**, **Bucket**, **Add bucket**
CLI: moin buckets add default \
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --bucket <bucket-name> \
  --access-key <access-key>
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

moin writes and deletes a test file first, so a wrong key fails at once. The secret key is stored encrypted.

## What needs it

| | Needs the bucket |
| --- | --- |
| `moin deploy` from your folder, the TUI or the moin GitHub Action | yes |
| [Catalog](/docs/guides/catalog) apps | yes |
| Backups, restores and **Download a copy** | yes |
| Deploys from [GitHub](/docs/guides/github) | no: the server downloads the commit itself |
| Image deploys from [CI](/docs/guides/ci) | no: the server pulls the image |

## See your buckets

```surfaces
Web: **Settings**, **Bucket**
CLI: moin buckets
TUI: opens the web: `w`, then `k`
API: GET /api/v1/buckets
MCP: list_buckets
```

## More than one bucket

Add another bucket the same way, with another name. Every app without a bucket of its own uses the default.

### Choose the bucket of an app

Moves the uploads and backups of one app to another bucket. Its copies start again there. What is in the old bucket stays.

```surfaces
Web: the app, **Data**, **Use another bucket**, then **Move**
CLI: moin buckets use client-a [app]
CLI: moin buckets use --default [app]
TUI: on the app, `W`, then **Data** in the web
API: PUT /api/v1/apps/{app}/bucket
MCP: set_app_bucket
```

### Change a bucket

Renames it, points it at another place, gives it new keys or makes it the default. An empty secret key keeps the stored one.

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

- The hosted hub only reaches buckets on the public internet.
- Two workspaces can share one bucket. Each gets its own folder.
- The `backups-to` [rule](/docs/guides/security#rules) of a server allows one bucket only.
- moin never deletes anything in a bucket you changed or removed. [How long things stay](/docs/concepts/backups#how-long-things-stay).
