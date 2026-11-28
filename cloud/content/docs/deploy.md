---
title: "Deploy"
description: "Deploy from your folder, from CI or from a registry. Roll back, restart, env vars, domains, logs and one-off commands."
group: "Apps"
order: 1
---

A deploy puts one version of your app on your server. Each deploy makes a release with a number: v1, v2, v3.

Commands about an app name the app: `moin logs shop`. moin knows on which server it runs. In the app's folder you may leave the name out: `moin logs`. The same app on two servers, like staging and prod? Add `-s prod`.

## Six ways to deploy

| You have | Command | Who builds |
| --- | --- | --- |
| A commit in your folder | `moin deploy prod` | the server |
| A branch on GitHub or GitLab | `moin follow shop --branch main` | the server, on each new commit |
| An image in your registry | `moin deploy prod --image ghcr.io/you/shop:1.4` | your CI. The server pulls it |
| A tag your CI pushes | `moin follow shop --image ghcr.io/you/shop:main` | your CI. The server pulls each new image |
| An app from the [catalog](/docs/catalog) | `moin deploy prod --catalog umami` | nobody. The image is ready |
| An image on your laptop | `moin deploy prod --image shop:dev` | your laptop. moin sends it over SSH |

Every way ends the same: a release with a number, a rollback, and a copy of the databases just before it.

## Deploy your code

In your app's folder:

```bash
moin deploy prod
```

```text
shop · 3f2a9c1 → prod
✓ sending the source
✓ detected compose, service web on port 3000
✓ building  9s
✓ starting  12s
✓ database /data/shop.db (/var/lib/moin/apps/shop/volumes/data/shop.db)
✓ shop v1 is live: https://shop.203-0-113-7.sslip.io
  SQLite found. See its backups: moin backups shop
```

moin sends the committed source of the folder to `prod`, over SSH. The server builds it and starts it. The app is named after the folder. Another name: `--app shop`. A name is lowercase letters, digits and dashes.

Every step shows a spinner while it runs, then a tick and how long it took. The output of Docker and of the build shows dimmed under the step. It goes away when the step is done, and stays when the step fails.

The first deploy to a server asks first, then installs Docker and moin there: `Set up moin on prod and deploy shop? [y/N]`. In a script, add `-y`.

The next time, `moin deploy` is enough: it deploys to the server the app runs on. On two servers, like staging and prod, name the one: `moin deploy prod`.

- Changes you did not commit stay on your laptop. moin says so: `Changes that are not committed stay here: moin sends 3f2a9c1.` Commit first.
- The source may be 200 MB at most.
- With a `Dockerfile` or a `compose.yml`, the server builds from those.
- Without either, moin builds the app with [Railpack](https://railpack.com). Node, Bun, Python, Ruby, PHP and Go work like that.
- The server builds. Your laptop needs no Docker, and you need no registry.
- An app that ran before gets a copy of its databases just before the new release starts. If that copy fails, the deploy stops and says why. `moin deploy --no-copy` deploys all the same. [Backups](/docs/backups#how-a-copy-is-made)

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

## Deploy an image from your registry

```bash
moin deploy prod --image ghcr.io/you/shop:1.4
```

```text
shop · 3f2a9c1 with image ghcr.io/you/shop:1.4 → prod
✓ pulling image ghcr.io/you/shop:1.4 on prod  2s
✓ sending the source
✓ detected compose, service web on port 8080
✓ building
✓ starting  8s
✓ shop v4 is live: https://shop.example.com
```

The server pulls the image and runs it. It builds nothing.

- Run it in the app's folder. The `compose.yml` and the settings come from your commit, as in any deploy. The image replaces the build of the service that gets the traffic.
- moin keeps each image by its content, not by its tag. A new `1.4` never changes a release that runs, and a rollback finds the old image.
- Use an image built for the server's CPU: `linux/amd64` for most servers, `linux/arm64` for ARM ones.

### Private registries

A public image needs nothing. For a private one, log the server in once:

```bash
moin registry set prod ghcr.io you
```

```text
? Token of you at ghcr.io:
✓ prod pulls from ghcr.io as you now
  The token is on prod only, in root's Docker config.
```

- moin asks for the token and does not show it. In a script, pipe it in: `echo "$TOKEN" | moin registry set prod ghcr.io you`. It is never a flag.
- Give the token read access only. On GitHub: a personal access token with `read:packages`.
- The server asks the registry right away. A wrong token is refused, and nothing is kept.
- It is a plain `docker login` as root. The login lives in `/root/.docker/config.json` on the server, for root only. Your laptop keeps nothing.
- Builds on the server use it too, like a `FROM` of a private image.
- `moin registry prod` lists the registries and the users, never a token. `moin registry rm prod ghcr.io` logs the server out.
- For Docker Hub the registry is `docker.io`.

## Deploy an image from your laptop

```bash
moin deploy prod --image shop:dev
```

moin sends the image you built on your laptop. The server builds nothing. This one needs Docker on your laptop.

The image on your laptop is what gets deployed. The server pulls it only when the registry has the very same one. Built `ghcr.io/you/shop:1.4` again and did not push it? moin sends your new one.

## Deploy from CI

The server pulls. CI needs no key to your server.

```bash
moin follow shop --branch main
```

```text
→ adding a read-only deploy key to you/shop with gh
✓ shop follows main. It deploys 3f2a9c1 within a minute, then each new commit
  Whoever can push to main deploys to prod. A deploy that fails alerts.
```

- The server makes a deploy key, `/var/lib/moin/follow/shop/key`. Only its public half leaves the server.
- With `gh` logged in, moin adds it to the GitHub repo by itself. Else it prints the key, and you add it as a deploy key with write access off.
- Every minute the server asks the repo for the newest commit of the branch. A new one is cloned and deployed like a deploy from your laptop.
- In the folder of the app, the branch and the repo come from git. Else name them: `--branch main --repo git@github.com:you/shop.git`.
- The repo goes over SSH, and only over SSH. An `https://` address becomes its SSH address. git on the server may use no other way, so a repo address can't run a command or read a file there.
- Each commit is tried once. A deploy that fails alerts once, and the next good one says it is over.
- `moin follow shop --off` stops it and removes the key from the server. Remove it from the repo too.

Whoever can push to that branch can deploy, so pick the branch with care.

## Deploy after your tests pass

`moin follow --branch` deploys every commit, green or not. To deploy only what passed, let CI build the image and let the server follow its tag:

```bash
moin follow shop --image ghcr.io/you/shop:main
```

```text
✓ shop follows ghcr.io/you/shop:main. It deploys 3f2a9c1e0b4d within a minute, then each new image under that tag
  Whoever can push to ghcr.io/you/shop:main deploys to prod. A deploy that fails alerts.
```

In CI, after the tests, build and push. With GitHub Actions:

```yaml
- run: go test ./...
- uses: docker/login-action@v3
  with:
    registry: ghcr.io
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
- uses: docker/build-push-action@v6
  with:
    push: true
    tags: ghcr.io/you/shop:main
```

- CI holds no key to your server. It pushes an image, as it does anyway. The server only goes out, to the registry.
- Every minute the server asks the registry whether the tag has a new image. Only a new one is pulled and deployed.
- Each image is tried once. A deploy that fails alerts once, and the next good one says it is over. The release before it keeps running.
- The app must be deployed once before it follows. The `compose.yml` and the settings stay those of your last `moin deploy`. To change them, deploy from your laptop again.
- A private image needs the server's login first: [Private registries](#private-registries).
- An app follows a branch or an image, not both. `moin follow shop --off` stops it.

Whoever can push to that tag can deploy, so guard the registry like the server.

## Roll back

```bash
moin rollback shop
```

moin lists the releases, and you pick one. Enter takes the one before the live one. In a script: `--to v3`, or nothing for the one before.

```text
DEPLOY   WHEN     STATE
v4       1m ago   ● live
v2       1m ago   the one before
v1       2m ago
? Go back to which deploy? [v2]
✓ rolling back shop
✓ rolling back to v2
✓ starting  22s
✓ shop runs v2 again, as v5
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
✓ starting shop with its domains  4s
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

`moin logs shop --json` prints them as a list, each with its time, its container and the line. It takes no `-f`.

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
