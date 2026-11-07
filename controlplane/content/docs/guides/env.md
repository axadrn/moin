---
title: "Env vars"
description: "Encrypted settings and secrets per app"
order: 10
---

Env vars are the settings and secrets of an app, such as `DATABASE_URL` or an API key. moin stores them encrypted and hands them to every service it builds on a restart, a start or the next deploy. Saving never restarts the app, so you can change several and restart once; the app page says how many changes wait. A rollback keeps the env vars of its release.

## See the env vars

Lists the names of an app's env vars.

```surfaces
Web: the app page, **Environment**
CLI: moin env
TUI: on the app, `e`
API: GET /api/v1/apps/{app}/env
MCP: get_env
```

## Show the values

Shows the values too, in plain text.

```surfaces
Web: the app page, **Environment**, **Show values**
CLI: moin env --show
TUI: on the app, `e`, then `v`
API: GET /api/v1/apps/{app}/env
MCP: get_env
```

Reading values needs an API token with write access: reading a secret is as strong as writing it. An AI agent that reads or sets values over MCP sees them, and so does the provider of its model: give it the secrets it needs, not all of them.

## In compose.yml

`${NAME}` in your `compose.yml` takes the env var from moin, like a `.env` next to the file would. So a service that is not built from your repo, such as Postgres, gets its secrets too, and none lands in the repo:

```yaml
services:
  db:
    image: postgres:17
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD}
```

Set it with `moin env set DB_PASSWORD=...`, then deploy. A change to such a variable takes a deploy, not just a restart.

## Set an env var

Adds an env var or changes its value.

```surfaces
Web: the app page, **Environment**, **Set**
CLI: moin env set KEY=value
TUI: on the app, `e`, `a`, then type `KEY=value`
API: PATCH /api/v1/apps/{app}/env
MCP: set_env
```

`moin env set` takes several at once: `moin env set KEY1=value KEY2=value`. Put quotes around a value with spaces: `moin env set GREETING="hello world"`. In the API, send them as `set`. Then restart the app: [`moin restart`](/docs/guides/deploy#restart). Until then the app runs with the old values.

## Remove an env var

Deletes an env var from the app.

```surfaces
Web: the app page, **Environment**, the × next to the env var
CLI: moin env rm KEY
TUI: on the app, `e`, pick the key, `x`, then `y`
API: PATCH /api/v1/apps/{app}/env
MCP: set_env
```

In the API and MCP, send the names as `unset`.

## Edit them all as one text

Shows every env var as one `.env` file, `KEY=value` per line, so you can paste a whole file. Saving it replaces all env vars: a key that is not in the text is removed. So moin first shows which keys it adds, changes and removes, by name only, and asks.

```surfaces
Web: the app page, **Environment**, switch **Fields** to **Text**, then **Review changes**
CLI: moin env push [file]
TUI: on the app, `e`, then `E`: the text opens in `$EDITOR`
API: PUT /api/v1/apps/{app}/env
MCP: replace_env
```

`moin env push` reads `.env` unless you name another file, and `--yes` skips the question. The TUI opens `$VISUAL` or `$EDITOR`, else `vi`, with a file only you can read; moin deletes it as soon as the editor closes. In the API and MCP, send every env var as `env`; the answer names the keys `added`, `changed` and `removed`.

The text takes `#` comments, `export KEY=value`, and values in quotes: in double quotes `\n` is a line break, in single quotes everything stays as it is. Then restart the app, as after every change.

## Use them on your computer

Runs your app locally with the same env vars as on the server:

```bash
moin env pull           # writes .env, readable only by you
moin run -- go run .    # runs a command with the env vars, without a file
```

Commands take the app from `--app`, from `x-moin.app` in [the repo](/docs/guides/config) or from the folder name.
