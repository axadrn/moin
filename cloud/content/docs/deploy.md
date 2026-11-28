---
title: "Deploy"
description: "Deploy from your folder or from CI or from a registry. Every deploy and its log. Roll back. Restart. Remove. Env vars. Domains. Logs. One-off commands. Run it elsewhere."
group: "Apps"
order: 1
---

A deploy puts one version of your app on your server. Each deploy makes a release with a number: v1, v2, v3.

Commands about an app name the app: `moin logs my-app`. moin knows on which server it runs. In the app's folder you may leave the name out: `moin logs`. The same app on two servers, like staging and production? Add `-s production`.

## Six ways to deploy

| You have | Command | Who builds |
| --- | --- | --- |
| A commit in your folder | `moin deploy my-server` | the server |
| A branch on GitHub or GitLab | `moin follow my-app --branch main` | the server, on each new commit |
| An image in your registry | `moin deploy my-server --image ghcr.io/you/my-app:1.4` | your CI. The server pulls it |
| A tag your CI pushes | `moin follow my-app --image ghcr.io/you/my-app:main` | your CI. The server pulls each new image |
| An app from the [catalog](/docs/catalog) | `moin deploy my-server --catalog umami` | nobody. The image is ready |
| An image on your laptop | `moin deploy my-server --image my-app:dev` | your laptop. moin sends it over SSH |

Every way ends the same: a release with a number, its log, a rollback, and a copy of the databases just before it.

## Deploy your code

In your app's folder:

```bash
moin deploy my-server
```

```text
my-app · 3f2a9c1 → my-server
✓ sending the source
✓ detected compose, service web on port 3000
✓ building  9s
✓ starting  12s
✓ database /data/my-app.db (/var/lib/moin/apps/my-app/volumes/data/my-app.db)
✓ my-app v1 is live: https://my-app.203-0-113-7.sslip.io
  SQLite found. See its backups: moin backups my-app
```

moin sends the committed source of the folder to `my-server`, over SSH. The server builds it and starts it. The app is named after the folder. Another name: `--app my-app`. A name is lowercase letters, digits and dashes.

Every step shows a spinner while it runs, then a tick and how long it took. The output of Docker and of the build shows dimmed under the step. It goes away when the step is done, and stays when the step fails.

The first deploy to a server asks first, then installs Docker and moin there: `Set up moin on my-server and deploy my-app? [y/N]`. In a script, add `-y`.

The next time, `moin deploy` is enough: it deploys to the server the app runs on. On two servers, like staging and production, name the one: `moin deploy production`.

- Changes you did not commit stay on your laptop. moin says so: `Changes that are not committed stay here: moin sends 3f2a9c1.` Commit first.
- A commit you did not push deploys too. moin says so: `! 3f2a9c1 is not on origin yet. Deploying it anyway.` With `--json`, the result says `"pushed": false`.
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
moin deploy my-server --image ghcr.io/you/my-app:1.4
```

```text
my-app · 3f2a9c1 with image ghcr.io/you/my-app:1.4 → my-server
✓ pulling image ghcr.io/you/my-app:1.4 on my-server  2s
✓ sending the source
✓ detected compose, service web on port 8080
✓ building
✓ starting  8s
✓ my-app v4 is live: https://my-app.example.com
```

The server pulls the image and runs it. It builds nothing.

- Run it in the app's folder. The `compose.yml` and the settings come from your commit, as in any deploy. The image replaces the build of the service that gets the traffic.
- moin keeps each image by its content, not by its tag. A new `1.4` never changes a release that runs, and a rollback finds the old image.
- Use an image built for the server's CPU: `linux/amd64` for most servers, `linux/arm64` for ARM ones.

### Private registries

A public image needs nothing. For a private one, log the server in once:

```bash
moin registry set my-server ghcr.io you
```

```text
? Token of you at ghcr.io:
✓ my-server pulls from ghcr.io as you now
  The token is on my-server only, in root's Docker config.
```

- moin asks for the token and does not show it. In a script, pipe it in: `echo "$TOKEN" | moin registry set my-server ghcr.io you`. It is never a flag.
- Give the token read access only. On GitHub: a personal access token with `read:packages`.
- The server asks the registry right away. A wrong token is refused, and nothing is kept.
- It is a plain `docker login` as root. The login lives in `/root/.docker/config.json` on the server, for root only. Your laptop keeps nothing.
- Builds on the server use it too, like a `FROM` of a private image.
- `moin registry my-server` lists the registries and the users, never a token. `moin registry rm my-server ghcr.io` logs the server out.
- For Docker Hub the registry is `docker.io`.

## Deploy an image from your laptop

```bash
moin deploy my-server --image my-app:dev
```

moin sends the image you built on your laptop. The server builds nothing. This one needs Docker on your laptop.

The image on your laptop is what gets deployed. The server pulls it only when the registry has the very same one. Built `ghcr.io/you/my-app:1.4` again and did not push it? moin sends your new one.

## Deploy from CI

The server pulls. CI needs no key to your server.

```bash
moin follow my-app --branch main
```

```text
→ adding a read-only deploy key to you/my-app with gh
✓ my-app follows main. It deploys 3f2a9c1 within a minute, then each new commit
  Whoever can push to main deploys to my-server. A deploy that fails alerts.
```

- The server makes a deploy key, `/var/lib/moin/follow/my-app/key`. Only its public half leaves the server.
- With `gh` logged in, moin adds it to the GitHub repo by itself. Else it prints the key, and you add it as a deploy key with write access off.
- Every minute the server asks the repo for the newest commit of the branch. A new one is cloned and deployed like a deploy from your laptop.
- In the folder of the app, the branch and the repo come from git. Else name them: `--branch main --repo git@github.com:you/my-app.git`.
- The repo goes over SSH, and only over SSH. An `https://` address becomes its SSH address. git on the server may use no other way, so a repo address can't run a command or read a file there.
- Each commit is tried once. A deploy that fails alerts once, with the command that shows its log, like `moin deploys my-app v7`. The next good one says it is over.
- `moin follow my-app --off` stops it and removes the key from the server. Remove it from the repo too.

Whoever can push to that branch can deploy, so pick the branch with care.

## Deploy after your tests pass

`moin follow --branch` deploys every commit, green or not. To deploy only what passed, let CI build the image and let the server follow its tag:

```bash
moin follow my-app --image ghcr.io/you/my-app:main
```

```text
✓ my-app follows ghcr.io/you/my-app:main. It deploys 3f2a9c1e0b4d within a minute, then each new image under that tag
  Whoever can push to ghcr.io/you/my-app:main deploys to my-server. A deploy that fails alerts.
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
    tags: ghcr.io/you/my-app:main
```

- CI holds no key to your server. It pushes an image, as it does anyway. The server only goes out, to the registry.
- Every minute the server asks the registry whether the tag has a new image. Only a new one is pulled and deployed.
- Each image is tried once. A deploy that fails alerts once, and the next good one says it is over. The release before it keeps running.
- The app must be deployed once before it follows. The `compose.yml` and the settings stay those of your last `moin deploy`. To change them, deploy from your laptop again.
- A private image needs the server's login first: [Private registries](#private-registries).
- An app follows a branch or an image, not both. `moin follow my-app --off` stops it.

Whoever can push to that tag can deploy, so guard the registry like the server.

## Every deploy

```bash
moin deploys my-app
```

```text
DEPLOY   WHEN      FROM             WHAT                  STATE      TOOK
v7       2m ago    follow           3f2a9c1               ✗ failed   41s
v6       1h ago    rollback to v4   my-app:main 0a1b2c3   ● live     12s
v5       3h ago    laptop           e4d5f6a               ok         1m 38s
v4       1d ago    follow           my-app:main 0a1b2c3   ok         25s
  Why it failed: moin deploys my-app v7
```

- FROM says where it came from: `laptop`, `follow`, `catalog`, a rollback, or `apply` for a restart, a start or new env and domains.
- WHAT is the code it runs: the commit, the image with the start of its ID, or the catalog app.
- `moin deploys my-app v7` prints what v7 printed, line by line, also when nobody watched it, like a deploy of a branch the server follows, or one whose SSH connection broke.
- `--json` prints the list, or with `v7` the record and its log. The record also names the copies of the databases taken just before.
- Each deploy keeps the last MB of its log, on the server, next to its release. A failed one keeps its log as long as the releases around it stay.
- Releases from before beta.39 show `no record`.

## Roll back

```bash
moin rollback my-app
```

moin lists the releases, and you pick one. Enter takes the one before the live one. In a script: `--to v3`, or nothing for the one before.

```text
DEPLOY   WHEN     WHAT      STATE
v4       1m ago   e4d5f6a   ● live
v2       1m ago   3f2a9c1   the one before
v1       2m ago   0a1b2c3
? Go back to which deploy? [v2]
✓ rolling back my-app
✓ rolling back to v2
✓ starting  22s
✓ my-app runs v2 again, as v5
```

A rollback is a new release: v2 runs again, as v4. It runs with the env vars and domains as they are now. It brings back the code, not the data. For the data, see [Restore](/docs/backups#restore).

## Restart. Stop. Start.

```bash
moin restart my-app
moin stop my-app
moin start my-app
```

A restart starts the live release again, with the env vars and domains as they are now. `moin start` does the same for a stopped app. A stopped app stays stopped, also after a reboot, and keeps its data.

You stopped it on purpose, so nothing alerts. The watcher shows it grey, not as a problem. `moin start` or a deploy ends that.

`moin apps` lists every app on every server, with its live release.

## Remove an app

```bash
moin apps rm my-app
```

```text
? Remove my-app from my-server? Its containers, images, env and domains go. Its volumes and its copies stay. [y/N] y
✓ removing my-app  3s
✓ my-app is gone from my-server. Its volumes stay there, with its data: a new deploy of my-app finds them
```

- It asks first. In a script, add `-y`.
- Its containers, images, releases, env vars and domains go. So does what it follows, with its deploy key.
- Its volumes stay, with the data. Deploy an app of the same name again and it finds them.
- Its copies in the bucket stay too. No new ones are made.

## Env vars

Settings and secrets of your app, like `DATABASE_URL`.

```bash
moin env my-app
moin env edit my-app
moin env set my-app API_KEY
moin env push my-app prod.env
```

- `moin env my-app` lists the names, never the values.
- `moin env edit my-app` opens your `$EDITOR` with the values, as a `.env` file. Save and close. The app restarts with them.
- A mistake in the file? moin says what, and opens it again. Quit without saving to keep the env as it was.
- The file lives in a folder only you can read, and is gone when the editor closes.
- `moin env set my-app API_KEY` sets one. moin asks for the value and does not show it. In a script, pipe it in: `echo "$API_KEY" | moin env set my-app API_KEY`. The value is never an argument, where your shell history would keep it.
- `moin env push my-app prod.env` replaces the env with the file, `.env` when you leave it out. `-` reads it from stdin. It lists what changes by name, `+` added, `~` changed, `-` removed, and asks first. In a script, add `-y`.

The env works before the first deploy too. An app that needs `DATABASE_URL` to start gets it first:

```bash
moin env set my-app DATABASE_URL -s my-server
moin deploy my-server
```

No server runs the app yet, so name the one it goes to with `-s`. On a terminal moin asks instead. The first deploy starts with the env.

They live in `/var/lib/moin/apps/<app>/app.env` on the server, readable only by root. They never pass through anything but your SSH connection.

In `compose.yml`, `${NAME}` takes the value from there.

## Domains

Every app gets an address right after the first deploy: `https://my-app.203-0-113-7.sslip.io`. It carries the server's IP, so it needs no DNS.

Add your own domain:

```bash
moin domains add my-app my-app.example.com
```

```text
✓ starting my-app with its domains  4s
✓ my-app answers on https://my-app.example.com
  Point the DNS of my-app.example.com to my-server: an A record with its IP. The certificate follows by itself.
```

Where you bought the domain, add an A record: the domain to your server's IPv4 address. If the server has IPv6, add an AAAA record too. HTTPS comes by itself, from Let's Encrypt, once the DNS points at the server.

```bash
moin domains my-app
moin domains rm my-app www.my-app.example.com
```

`moin domains my-app` lists them. `moin domains rm` asks first, because visitors there get no answer. In a script, add `-y`.

Add `example.com` and `www.example.com` both, and www redirects to `example.com` for good. The path and the query stay. `example.com` serves the app.

## Password

Keep visitors out of an app, like a staging site or an admin tool without a login of its own:

```bash
moin password my-app
```

```text
? Password for my-app:
? Once more:
✓ starting my-app with its password  4s
✓ my-app asks every visitor for a password now. The user is moin
```

- The browser asks for a user and the password, on every address of the app: its domains and its instant address.
- The user is `moin`. `--user ada` names another.
- moin asks for the password and does not show it. A script pipes it in: `echo "$PASSWORD" | moin password my-app`. It never goes on the command line.
- The server keeps a bcrypt hash of it, in a file only root reads. Never the password.
- Run it again for a new password. The app restarts, without a build.
- `moin apps` shows `password on` next to the app.

```bash
moin password off my-app
```

Everyone gets in again.

Webhooks and API clients of the app need the password too. They send it as HTTP basic auth, like `https://moin:<password>@my-app.example.com/hook`. One that cannot, like the webhook of a payment provider, gets a 401. So does a client that sends its own `Authorization` header, like a bearer token: that header holds the password now.

A [watcher](/docs/watcher) counts the 401 as up: the app answered.

## Logs

```bash
moin logs my-app
moin logs my-app -f
moin logs my-app --tail 1000
```

The last 200 lines of the app. `-f` follows them as they come, until Ctrl-C.

`moin logs my-app --json` prints them as a list, each with its time, its container and the line. It takes no `-f`.

Logs never fill the disk. Each service keeps three files of 10 MB, the oldest goes. A service with its own `logging:` keeps it, with the `json-file` or `local` driver. Another driver would send the logs off the server, so it needs `server_access: true` in the [config](/docs/config).

## Run a command

A one-off command in the app's container, like a migration:

```bash
moin exec my-app -- bin/rails db:migrate
```

Everything after `--` is the command. It runs once and prints what it prints. On a terminal it gets a terminal, so a shell works: `moin exec my-app -- sh`. moin exits with the command's exit code.

It runs in the app's main service. Another one: `moin exec my-app --service db -- psql -U postgres`.

## Run it elsewhere

`moin compose` prints the compose file moin runs for the app: your services, plus the proxy that serves your domains with HTTPS. It is your way out. Nothing in it needs moin.

```bash
moin compose my-app > compose.yml
```

- It holds no value of your env. Every `${NAME}` stays as you wrote it. The settings the deploy filled in show the value they ran with: `image`, `ports`, `expose`, `volumes`, `user`, the env `PORT` and the paths of a `build`.
- Before the server sends it, it checks the file for every env value of 6 characters or more. A file that holds one stays on the server.
- It is the file of the live deploy. `--json` gives the app, the release and the file.
- In the terminal UI, `C` shows it in your `$PAGER`.

To run the app on any server with Docker:

1. Save it as `compose.yml` in a folder, next to your repo's files when it builds from them.
2. Put a `.env` next to it with your env vars, and the values of the `.env` in your repo if it has one. Docker fills `${NAME}` from it, and the app reads it too.
3. Point your domains at the new server.
4. Run `docker compose up -d`.

The proxy takes ports 80 and 443 and gets the certificates from Let's Encrypt, as on your server. It does that also when your server gets its traffic through a [Cloudflare Tunnel](/docs/basics/cloudflare-tunnel).

Each file brings its own proxy. For several apps on one server, keep the proxy of one file, delete it from the others, and write `external: true` under `moin-proxy` in their `networks`.

What changed on the way out:

- **Source**: the folder of the file is your repo, `.`. A service moin built has `build: .` again.
- **Data**: the folders moin kept outside each release are where your `compose.yml` put them, like `./data`. Copy the data there first. Named volumes keep their names.
- **Instant address**: it stays with moin. It names your server's IP, so the file routes only your domains.
- **Password**: it stays with moin. Its file is on your server, so the file lets everyone in.
- **Railpack apps**: their build plan is not in your repo. The file keeps the image moin built, which is only on your server. Run `railpack build .` in your repo there and put the image's name in the file, or push the image to a registry.

## If it breaks

- **The build fails.** moin shows the build output. The live release keeps running. `moin deploys my-app v7` shows it again later.
- **The new release does not come up.** moin starts the release before it again, and shows the app's last logs. `moin logs my-app` shows more.
- **A bad deploy is live.** `moin rollback my-app`.
