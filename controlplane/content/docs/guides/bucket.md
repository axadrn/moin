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
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --bucket <bucket-name> \
  --access-key <access-key>
TUI: settings tab, **Bucket**, then `enter`
API: PUT /api/v1/backup-storage
MCP: set_backup_storage
```

Any S3-compatible storage works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner Object Storage, Exoscale, MinIO. Pick one in the country your data should stay in.

Before you add it:

1. Create a bucket in your storage provider's dashboard.
2. Create an access key that may read, write and delete in that bucket. You get two parts: the access key (its ID) and the secret key.
3. Look up the endpoint, the web address of the storage. Your provider shows it next to the bucket. For Cloudflare R2 it is `https://<account-id>.r2.cloudflarestorage.com`.

What ops needs:

| Field | What to type |
| --- | --- |
| Endpoint | The endpoint. It must start with `https://`. Only `http://localhost` and `http://127.0.0.1` may use plain `http://`, on a self-hosted control plane. For AWS S3, leave it empty. |
| Bucket | The name of the bucket. |
| Folder | Usually empty. See [The folder field](#the-folder-field). |
| Access key, Secret key | The two parts of the key. The CLI asks for the secret key on stdin, so it never ends up in your shell history. |
| Region | `auto` for Cloudflare R2. For AWS S3, the region of the bucket, like `eu-central-1`. Otherwise what your provider says. |

ops writes and deletes a test file first, so a wrong key fails right away. The hosted control plane only reaches buckets on the public internet, not on a private or local address.

## What goes in the bucket

Everything of one workspace goes into one folder, named after the workspace's ID. Inside it each app gets its own folder. So two workspaces can share one bucket, even with apps of the same name, and never touch each other's files.

With only a bucket name:

```
<your-bucket>/
└── <workspace>/                  ← your workspace's ID, it never changes
    └── <app-name>/               ← the name of your app
        └── <app-id>/             ← this app's ID, a new app with the same name gets a new one
            ├── uploads/          ← your code on its way to the server
            └── <database-file>/  ← one per database (e.g. data-app.db for /data/app.db)
                ├── litestream/   ← every change, any second of the last 7 days
                └── snapshots/    ← full copies, the newest 50
```

With a bucket name and a folder, everything moves one level down, into the folder:

```
<your-bucket>/
└── <folder>/                         ← the folder you typed into the Folder field
    └── <workspace>/                  ← your workspace's ID, it never changes
        └── <app-name>/               ← the name of your app
            └── <app-id>/             ← this app's ID, a new app with the same name gets a new one
                ├── uploads/          ← your code on its way to the server
                └── <database-file>/  ← one per database (e.g. data-app.db for /data/app.db)
                    ├── litestream/   ← every change, any second of the last 7 days
                    └── snapshots/    ← full copies, the newest 50
```

What each folder holds:

- **uploads:** your code on its way to the server. Temporary: deleted as soon as the server has it, and anything left over is deleted after 24 hours.
- **litestream:** every change as it happens, so you can go back to any second of the last 7 days.
- **snapshots:** full copies before every deploy and change, the last 50. Each one is a plain gzipped SQLite file.

The **Bucket** page shows your workspace's folder. The **Backups** tab of an app shows the app's folder.

## How long things stay

| What | How long it stays | Who deletes it |
| --- | --- | --- |
| `uploads/` | until the server has it, 24 hours at most | the ops agent on your server: right after the download, and once an hour anything older than 24 hours |
| `litestream/` | every change of the last 7 days | the ops agent on your server, with Litestream's own cleanup: older changes go as new ones come in |
| `snapshots/` | the newest 50 per database | the ops agent on your server: after each new snapshot it deletes the oldest beyond 50 |
| an app you removed | until you delete it | you: delete its `<app-id>/` folder in your storage provider's dashboard or with any S3 tool |
| a bucket or folder you changed | until you delete it | you: ops starts in the new place with a full copy and leaves the old place as it is |
| a bucket you removed from ops | until you delete it | you: backups stop and ops deletes nothing |

Remove an app and create a new one with the same name, and the new app starts with an empty folder: old backups never mix with new ones. The old app's backups stay in its own `<app-id>/` folder. To get its data back, download a snapshot from there, `gunzip` it and open it with `sqlite3`.

## The folder field

The **Folder** field on the Bucket page is `--path` in the CLI and `path` in the API and MCP. Most people leave it empty. Fill it in when the bucket holds other files too, such as a bucket you already use for something else. Everything of ops then stays inside that one folder, like `ops/`, and the rest of the bucket is left alone.

You do not need it to share one bucket between workspaces: each workspace gets its own folder anyway.

## Next

- [Backups](/docs/guides/backups): what ops keeps in the bucket and how to restore it.
- [Deploy](/docs/guides/deploy#deploy-from-a-repo): how `ops deploy` puts your code into it.
- [Stop backups](/docs/guides/backups#stop-backups): remove the bucket from ops.
