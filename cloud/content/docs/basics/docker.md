---
title: "Docker and compose"
description: "Image, container, volume and compose.yml in one page. How your app runs on the server, and how moin builds an app that has no Dockerfile."
group: "Basics"
order: 8
---

Your app needs things to run. A language, like Node or Python. Libraries. Settings. On your laptop they are all there. On a fresh server, none are.

Docker packs your app and everything it needs into one box. The box runs the same on every Linux server.

## Four words

```text
Dockerfile ──build──▶ image ──run──▶ container ──writes──▶ volume
  recipe              box            box, running          data that stays
```

- **Dockerfile**: the recipe. Start from Node 24. Copy the code. Install the libraries. Start with `npm start`.
- **Image**: the box the recipe makes. It holds your app and all it needs. It never changes.
- **Container**: an image that runs. You can start, stop and throw away a container. A new one from the same image starts the same.
- **Volume**: a folder that outlives containers. Your data goes there: a SQLite file, Postgres's files, uploads.

A container is thrown away with each new release. Anything it wrote outside a volume is gone. So data always lives in a volume.

## A Dockerfile

```dockerfile
FROM node:24-slim
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
CMD ["npm", "start"]
```

Each line is a step. `FROM` is the start, a ready image someone else made. `CMD` is what runs when the container starts.

## compose.yml

Most apps are more than one box. An app and its database. Compose describes them together, in one file:

```yaml
services:
  web:
    build: .
    environment:
      DATABASE_URL: postgres://postgres:${POSTGRES_PASSWORD}@db:5432/postgres
    depends_on: [db]
  db:
    image: postgres:17
    environment:
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - pgdata:/var/lib/postgresql/data
volumes:
  pgdata:
```

- `services` are the boxes. Here `web`, built from your Dockerfile, and `db`, a ready Postgres image.
- They reach each other by name. The app finds Postgres at `db:5432`.
- `pgdata` is a named volume. Postgres's files outlive every release.
- `${POSTGRES_PASSWORD}` comes from the env. With moin: `moin env edit shop`.

No `ports` on `db`. Only the app reaches it. A published port would open it to the internet. [Ports and the firewall](/docs/basics/ports#docker-walks-past-ufw)

## What moin does with it

You don't run Docker yourself. moin installs it on the server, and runs it for you. Your laptop needs no Docker.

```bash
moin deploy prod
```

moin sends your committed code. The server builds it, in this order:

1. **A `compose.yml`?** moin uses it as it is. `compose.yaml`, `docker-compose.yml` and `docker-compose.yaml` work too.
2. **A `Dockerfile`?** moin builds it, as one service named `web`.
3. **A `go.mod`?** moin writes a Dockerfile for Go itself.
4. **None of these?** moin builds with [Railpack](https://railpack.com). It reads your code, finds the language and its start command, and builds the image. Node, Bun, Python, Ruby, PHP and more.

The deploy says what it found:

```text
→ Railpack: Node 24.11.1, start: npm run start
```

Without a compose file, the app gets two settings from moin:

- `PORT=8080`. Listen there.
- `DATA_DIR=/data`, a volume. Put a SQLite file there, and moin backs it up.

## What moin reads from compose

Only Docker's own fields. Which service gets the traffic: the only one, or the one named `web`. Its port: from `ports` or `expose`. Its data: from `volumes`. No moin labels, no moin file. [What moin reads from compose.yml](/docs/deploy#what-moin-reads-from-composeyml)

Your app stays plain Docker Compose. It runs without moin too.

## Look inside

On the server:

```bash
docker ps
docker logs <container>
```

From your laptop, the same through moin:

```bash
moin logs shop
moin exec shop -- sh
```
