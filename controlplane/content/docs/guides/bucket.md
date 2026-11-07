---
title: "Your buckets"
description: "Your code and your backups live in your own storage, never with us"
order: 2
---

**The short version:** you bring a storage bucket, or several. A bucket is a folder in an online storage service such as Cloudflare R2 or AWS S3. Your code and your backups go in there, and nothing of yours is ever stored by moin. One bucket serves every app; [more than one](#more-than-one-bucket) keeps a client's data in the client's bucket. moin only signs short-lived links into it.

## Why a bucket

Your laptop and your server never talk to each other directly: the server has no open port, your laptop sits behind a router. They need a place in the middle to hand things over. That place is your bucket, not our servers.

```
moin deploy   laptop ──▶ your bucket ──▶ your server     (the server deletes it after)
backups      server ──▶ your bucket                      (every change, plus snapshots)
GitHub       GitHub ──────────────────▶ your server     (no bucket needed)
```

moin keeps the bucket's secret key encrypted and uses it for one thing: to sign links that work for a few minutes. Your code and your data never pass through moin.

## What needs it

| | Needs the bucket |
| --- | --- |
| `moin deploy` from your laptop, the TUI or the moin GitHub Action | yes |
| Backups and restores | yes |
| Deploys from [GitHub](/docs/guides/github) | no: the server downloads the commit from GitHub itself |
| Image deploys from [CI](/docs/guides/ci) | no: the server pulls the image from your registry |

## Add it

Tells moin which bucket to use. Do it once per workspace: one bucket serves all your servers and apps. The first bucket is the workspace's default.

```surfaces
Web: **Settings**, **Bucket**, then **Add bucket**
CLI: moin buckets add default \
  --endpoint https://<account-id>.r2.cloudflarestorage.com \
  --bucket <bucket-name> \
  --access-key <access-key>
TUI: opens the web: `w`, then `k`
API: POST /api/v1/buckets
MCP: add_bucket
```

Any S3-compatible storage works: Cloudflare R2, AWS S3, Backblaze B2, Hetzner Object Storage, Exoscale, MinIO. Pick one in the country your data should stay in.

Before you add it:

1. Create a bucket in your storage provider's dashboard.
2. Create an access key that may read, write and delete in that bucket. You get two parts: the access key (its ID) and the secret key.
3. Look up the endpoint, the web address of the storage. Your provider shows it next to the bucket. For Cloudflare R2 it is `https://<account-id>.r2.cloudflarestorage.com`.

What moin needs:

| Field | What to type |
| --- | --- |
| Name | Your name for it in moin, like `default` or `client-a`: lowercase letters, digits and dashes. The CLI takes it as the first argument. |
| Endpoint | The endpoint. It must start with `https://`. Only `http://localhost` and `http://127.0.0.1` may use plain `http://`, on a self-hosted control plane. For AWS S3, leave it empty. |
| Bucket | The name of the bucket. |
| Folder | Usually empty. See [The folder field](#the-folder-field). |
| Access key, Secret key | The two parts of the key. The CLI asks for the secret key on stdin, so it never ends up in your shell history. |
| Region | `auto` for Cloudflare R2. For AWS S3, the region of the bucket, like `eu-central-1`. Otherwise what your provider says. |

moin writes and deletes a test file first, so a wrong key fails right away. To make a server back up only to this bucket, see the `backups-to` [rule](/docs/guides/security#rules). The hosted control plane only reaches buckets on the public internet, not on a private or local address.

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
- **snapshots:** full copies before every deploy and change, the last 50. Each one is a plain gzipped SQLite file, or for Postgres a `pg_dump` you open with `pg_restore`.
- **walg:** for Postgres, every change and the base backups, kept by [WAL-G](https://github.com/wal-g/wal-g) for 7 days.

The **Buckets** page shows your workspace's folder in each bucket. The **Backups** tab of an app shows the app's folder and its bucket.

## How long things stay

| What | How long it stays | Who deletes it |
| --- | --- | --- |
| `uploads/` | until the server has it, 24 hours at most | the moin agent on your server: right after the download, and once an hour anything older than 24 hours |
| `litestream/` | every change of the last 7 days | the moin agent on your server, with Litestream's own cleanup: older changes go as new ones come in |
| `snapshots/` | the newest 50 per database | the moin agent on your server: after each new snapshot it deletes the oldest beyond 50 |
| an app you removed | until you delete it | you: delete its `<app-id>/` folder in your storage provider's dashboard or with any S3 tool |
| a bucket or folder you changed | until you delete it | you: moin starts in the new place with a full copy and leaves the old place as it is |
| a bucket you removed from moin | until you delete it | you: moin deletes nothing |
| an app you moved to another bucket | until you delete it | you: its older backups stay in the old bucket, and moin still lists and restores them from there |

Remove an app and create a new one with the same name, and the new app starts with an empty folder: old backups never mix with new ones. The old app's backups stay in its own `<app-id>/` folder. To get its data back, download a snapshot from there, `gunzip` it and open it with `sqlite3`.

## More than one bucket

A workspace can keep several buckets, each with its own name. Reasons to add another one:

- a client's data in the client's own bucket,
- a bucket in another region, near the server,
- separate costs per project,
- production backups apart from test apps.

One bucket is the **default**: every app without a bucket of its own uploads and backs up there, and so does every new app. The first bucket you add is the default; a bucket from before moin had more than one is the default, named `default`.

### See your buckets

Lists every bucket, the default first, with the apps that use it and the apps that keep older backups in it. Secret keys never show.

```surfaces
Web: **Settings**, **Bucket**
CLI: moin buckets
TUI: opens the web: `w`, then `k`
API: GET /api/v1/buckets
MCP: list_buckets
```

### Choose the bucket of an app

Moves the uploads and the new backups of one app to another bucket. Without a choice an app follows the default.

```surfaces
Web: the app page, **Backups** tab, **Use another bucket**, then **Move**
CLI: moin buckets use client-a [app]
CLI: moin buckets use --default [app]
TUI: on the app, `W`, then the **Backups** tab in the web
API: PUT /api/v1/apps/{app}/bucket
MCP: set_app_bucket
```

What happens to the backups of the app:

- **New backups** go to the new bucket. It starts with a full copy of every database, then every change, so a point in time restore reaches back to the move and no further.
- **Older backups stay in the old bucket.** moin moves and deletes nothing. The backups list of the app shows the older snapshots too, with the bucket they are in.
- **Restores read from where a backup lives.** Restoring an older snapshot downloads it from the old bucket: on the web **Restore** on the snapshot, in the CLI `moin restore <snapshot> --bucket <old bucket>`. A point in time restore always reads from the app's bucket now.
- **Uploads** of `moin deploy` go to the new bucket right away.

Making another bucket the default moves every app that follows the default the same way. The rule [`backups-to`](/docs/guides/security#rules) of a server allows one bucket: an app on that server in another bucket is refused there.

A server's agent needs this release or newer to back up an app to its own bucket. Older agents back up every app to the default bucket; [agents update themselves](/docs/guides/servers) unless you turned that off.

### Change a bucket

Renames a bucket, points it at another place, gives it new keys or makes it the default. Leave the secret key empty to keep the stored one. A new place starts with a full copy, and the old place keeps what it has.

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

Removes a bucket from moin. What is in it stays there.

```surfaces
Web: **Settings**, **Bucket**, **Remove** on the bucket, then type its name
CLI: moin buckets rm client-a
TUI: opens the web: `w`, then `k`
API: DELETE /api/v1/buckets/{name}
MCP: remove_bucket
```

moin refuses to remove a bucket that is still in use, and names the apps:

- **Apps use it**: their uploads and backups go there, the default's also for every app without a bucket of its own. Move them to another bucket first.
- **It keeps older backups** of an app that moved away. Restores of those read from there, so the bucket stays while the app exists.

A workspace without apps can remove its last bucket. moin asks you to type the bucket's name first; `--yes` skips it in scripts.

## The folder field

The **Folder** field on the Buckets page is `--path` in the CLI and `path` in the API and MCP. Most people leave it empty. Fill it in when the bucket holds other files too, such as a bucket you already use for something else. Everything of moin then stays inside that one folder, like `moin/`, and the rest of the bucket is left alone.

You do not need it to share one bucket between workspaces: each workspace gets its own folder anyway.

## Next

- [Backups](/docs/guides/backups): what moin keeps in the bucket and how to restore it.
- [Deploy](/docs/guides/deploy#deploy-from-a-repo): how `moin deploy` puts your code into it.
- [Remove a bucket](#remove-a-bucket): what moin refuses, and why.
