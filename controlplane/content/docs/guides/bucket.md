---
title: "Your bucket"
description: "Your code and your backups live in your own storage, never with us"
order: 2
---

**The short version:** you bring one storage bucket. A bucket is a folder in an online storage service such as Cloudflare R2 or AWS S3. Your code and your backups go in there, and nothing of yours is ever stored by ops. ops only signs short-lived links into it.

## Why a bucket

Your laptop and your server never talk to each other directly: the server has no open port, your laptop sits behind a router. They need a place in the middle to hand things over. That place is your bucket, not our servers.

```
ops deploy   laptop ──▶ your bucket ──▶ your server     (the server deletes it after)
backups      server ──▶ your bucket                      (every change, plus snapshots)
GitHub       GitHub ──────────────────▶ your server     (no bucket needed)
```

ops keeps the bucket's secret key encrypted and uses it for one thing: to sign links that work for a few minutes. Your code and your data never pass through ops.

## What needs it

| | Needs the bucket |
| --- | --- |
| `ops deploy` from your laptop, the TUI or the ops GitHub Action | yes |
| Backups and restores | yes |
| Deploys from [GitHub](/docs/guides/github) | no: the server downloads the commit from GitHub itself |
| Image deploys from [CI](/docs/guides/ci) | no: the server pulls the image from your registry |

## Add it

Tells ops which bucket to use. Do it once per workspace: one bucket serves all your servers and apps.

```surfaces
Web: **Bucket** in the sidebar, then **Save bucket**
CLI: ops backups bucket set \
  --endpoint https://<account>.r2.cloudflarestorage.com \
  --bucket my-ops \
  --access-key <key id>
TUI: settings tab, **Bucket**, then `enter`
API: PUT /api/v1/backup-storage
MCP: set_backup_storage
```

Any S3-compatible storage works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner Object Storage, Exoscale, MinIO. Pick one in the country your data should stay in. Create a bucket and a key that may read, write and delete in it first. The CLI asks for the secret key on stdin.

ops writes and deletes a test file first, so a wrong key fails right away. In the bucket each app gets a folder:

```
<folder>/<app>/uploads/      code on its way to the server, gone after the deploy starts
<folder>/<app>/data-app.db/  backups of /data/app.db
```
