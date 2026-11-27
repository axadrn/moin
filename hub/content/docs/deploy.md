---
title: "Deploy"
description: "Deploy from your folder or a local image. Roll back, restart, env vars, domains, logs and one-off commands."
group: "Apps"
order: 1
---

A deploy puts one version of your app on your server. Each deploy gets a number: v1, v2, v3.

Commands about an app name the app: `moin logs shop`. moin knows on which server it runs. In the app's folder you may leave the name out: `moin logs`. The same app on two servers, like staging and prod? Add `-s prod`.

## Deploy your code

In your app's folder:

```bash
moin deploy prod
```

moin sends the committed source of the folder to `prod`, over SSH. The server builds it and starts it. The app is named after the folder. Another name: `--app shop`.

The next time, `moin deploy` is enough: it deploys to the server the app runs on. On two servers, like staging and prod, name the one: `moin deploy prod`.

- Changes you did not commit stay on your laptop. Commit first.
- With a `Dockerfile` or a `compose.yml`, the server builds from those.
- Without either, moin builds the app with [Railpack](https://railpack.com). Node, Bun, Python, Ruby, PHP and Go work like that.
- The server builds. Your laptop needs no Docker, and you need no registry.

## What moin reads from compose.yml

`compose.yml` is plain Docker. moin reads Docker's own fields, nothing else. `compose.yaml` and `docker-compose.yml` work too.

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

moin lists the deploys, and you pick one. The one before the live one is first. In a script: `--to v3`. A rollback brings back the code, not the data. For the data, see [Restore](/docs/backups#restore).

## Restart, stop and start

```bash
moin restart shop
moin stop shop
moin start shop
```

A restart starts the live deploy again, with the newest env vars. A stopped app keeps its data.

`moin apps` lists every app on every server, with its live deploy.

## Env vars

Settings and secrets of your app, like `DATABASE_URL`.

```bash
moin env shop
moin env edit shop
```

- `moin env shop` lists the names, never the values.
- `moin env edit shop` opens your `$EDITOR` with the values, as a `.env` file. Save and close.
- Then `moin restart shop`, and the app runs with them.

They live in `/var/lib/moin/apps/<app>/app.env` on the server, readable only by root. They never pass through anything but your SSH connection.

In `compose.yml`, `${NAME}` takes the value from there.

## Domains

Every app gets an address right after the first deploy: `https://shop.203-0-113-7.sslip.io`. It carries the server's IP, so it needs no DNS.

Add your own domain:

```bash
moin domains add shop shop.example.com
moin domains add shop www.shop.example.com
```

Then, where you bought the domain, add an A record: the domain to your server's IPv4 address. If the server has IPv6, add an AAAA record too. HTTPS comes by itself, from Let's Encrypt, once the DNS points at the server.

```bash
moin domains shop
moin domains rm shop www.shop.example.com
```

`moin domains shop` lists them and says if each one points at the server yet.

## Logs

```bash
moin logs shop
moin logs shop -f
moin logs shop --tail 1000
```

The last 200 lines of the app. `-f` follows them as they come.

## Run a command

A one-off command in the app's container, like a migration:

```bash
moin exec shop -- bin/rails db:migrate
```

Everything after `--` is the command. It runs once and prints what it prints.

## If it breaks

- **The build fails.** moin shows the build output. The live deploy keeps running.
- **The new deploy does not come up.** moin starts the deploy before it again, and says why. `moin logs shop` shows what the app said.
- **A bad deploy is live.** `moin rollback shop`.
