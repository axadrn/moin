---
title: "Deploy"
description: "Deploy from your folder or a local image. Roll back, restart, env vars, domains, logs and one-off commands."
group: "Apps"
order: 1
---

A deploy puts one version of your app on your server. Each deploy makes a release with a number: v1, v2, v3.

Commands about an app name the app: `moin logs shop`. moin knows on which server it runs. In the app's folder you may leave the name out: `moin logs`. The same app on two servers, like staging and prod? Add `-s prod`.

## Deploy your code

In your app's folder:

```bash
moin deploy prod
```

```text
shop · 3f2a9c1 → prod
→ building
→ starting
→ v1 is live
✓ moin. shop is live: https://shop.203-0-113-7.sslip.io
  SQLite found. See its backups: moin backups shop
```

moin sends the committed source of the folder to `prod`, over SSH. The server builds it and starts it. The app is named after the folder. Another name: `--app shop`. A name is lowercase letters, digits and dashes.

The first deploy to a server asks first, then installs Docker and moin there: `Set up moin on prod and deploy shop? [y/N]`. In a script, add `-y`.

The next time, `moin deploy` is enough: it deploys to the server the app runs on. On two servers, like staging and prod, name the one: `moin deploy prod`.

- Changes you did not commit stay on your laptop. moin says so: `Changes that are not committed stay here: moin sends 3f2a9c1.` Commit first.
- The source may be 200 MB at most.
- With a `Dockerfile` or a `compose.yml`, the server builds from those.
- Without either, moin builds the app with [Railpack](https://railpack.com). Node, Bun, Python, Ruby, PHP and Go work like that.
- The server builds. Your laptop needs no Docker, and you need no registry.

## What moin reads from compose.yml

`compose.yml` is plain Docker. moin reads Docker's own fields, nothing else. `compose.yaml`, `docker-compose.yml` and `docker-compose.yaml` work too.

```yaml
services:
  web:
    build: .
    expose: ["8080"]
    volumes:
      - ./data:/data
```

- **The service that gets the traffic**: the only one, else the only one moin builds, else the one named `web`.
- **Its port**: from `ports` or `expose`. Else its env `PORT`, then `EXPOSE` in the `Dockerfile`, then 8080.
- **When it is ready**: its `healthcheck`, if it has one. It must also run 10 seconds without a restart.
- **Its data**: `volumes`. moin keeps them outside each deploy, so a new deploy finds the same data.

An app stays in its own room. It may mount its own folders, like `./data`, and its own named volumes. A setting that reaches beyond the app, like `privileged: true` or a mount of the Docker socket, is refused, and the deploy says why. The server's [config](/docs/config#mounts-and-server_access) can allow more.

## Deploy an image from your laptop

```bash
moin deploy prod --image shop:dev
```

moin sends the image you built on your laptop. The server builds nothing. This one needs Docker on your laptop.

## Deploy from CI

Coming: `moin follow`. The server watches a branch with a read-only key and deploys each new commit. CI needs no key to your server. Whoever can push to that branch can deploy, so pick the branch with care.

## Roll back

```bash
moin rollback shop
```

moin lists the releases, and you pick one. Enter takes the one before the live one. In a script: `--to v3`, or nothing for the one before.

```text
✓ shop runs v2 again, as v4
```

A rollback is a new release: v2 runs again, as v4. It runs with the env vars and domains as they are now. It brings back the code, not the data. For the data, see [Restore](/docs/backups#restore).

## Restart, stop and start

```bash
moin restart shop
moin stop shop
moin start shop
```

A restart starts the live release again, with the env vars and domains as they are now. `moin start` does the same for a stopped app. A stopped app stays stopped, also after a reboot, and keeps its data.

`moin apps` lists every app on every server, with its live release.

## Env vars

Settings and secrets of your app, like `DATABASE_URL`.

```bash
moin env shop
moin env edit shop
```

- `moin env shop` lists the names, never the values.
- `moin env edit shop` opens your `$EDITOR` with the values, as a `.env` file. Save and close. The app restarts with them.
- A mistake in the file? moin says what, and opens it again. Quit without saving to keep the env as it was.
- The file lives in a folder only you can read, and is gone when the editor closes.

They live in `/var/lib/moin/apps/<app>/app.env` on the server, readable only by root. They never pass through anything but your SSH connection.

In `compose.yml`, `${NAME}` takes the value from there.

## Domains

Every app gets an address right after the first deploy: `https://shop.203-0-113-7.sslip.io`. It carries the server's IP, so it needs no DNS.

Add your own domain:

```bash
moin domains add shop shop.example.com
```

```text
✓ shop answers on https://shop.example.com
  Point the DNS of shop.example.com to prod: an A record with its IP. The certificate follows by itself.
```

Where you bought the domain, add an A record: the domain to your server's IPv4 address. If the server has IPv6, add an AAAA record too. HTTPS comes by itself, from Let's Encrypt, once the DNS points at the server.

```bash
moin domains shop
moin domains rm shop www.shop.example.com
```

`moin domains shop` lists them. `moin domains rm` asks first, because visitors there get no answer. In a script, add `-y`.

## Logs

```bash
moin logs shop
moin logs shop -f
moin logs shop --tail 1000
```

The last 200 lines of the app. `-f` follows them as they come, until Ctrl-C.

## Run a command

A one-off command in the app's container, like a migration:

```bash
moin exec shop -- bin/rails db:migrate
```

Everything after `--` is the command. It runs once and prints what it prints. On a terminal it gets a terminal, so a shell works: `moin exec shop -- sh`. moin exits with the command's exit code.

It runs in the app's main service. Another one: `moin exec shop --service db -- psql -U postgres`.

## If it breaks

- **The build fails.** moin shows the build output. The live release keeps running.
- **The new release does not come up.** moin starts the release before it again, and shows the app's last logs. `moin logs shop` shows more.
- **A bad deploy is live.** `moin rollback shop`.
