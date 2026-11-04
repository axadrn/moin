---
title: "Env vars"
description: "Encrypted settings and secrets per app"
order: 10
---

Env vars are the settings and secrets of an app, such as `DATABASE_URL` or an API key. ops stores them encrypted and hands them to every service it builds on a restart, a start or the next deploy. So after you change one, restart the app. A rollback keeps the env vars of its release.

## See the env vars

Lists the names of an app's env vars.

```surfaces
Web: the app page, **Environment**
CLI: ops env
TUI: on the app, `e`
API: GET /api/v1/apps/{app}/env
MCP: get_env
```

## Show the values

Shows the values too, in plain text.

```surfaces
Web: the app page, **Environment**, **Show values**
CLI: ops env --show
TUI: on the app, `e`, then type `show`
API: GET /api/v1/apps/{app}/env
MCP: get_env
```

Reading values needs an API token with write access: reading a secret is as strong as writing it. An AI agent that reads or sets values over MCP sees them, and so does the provider of its model: give it the secrets it needs, not all of them.

## In compose.yml

`${NAME}` in your `compose.yml` takes the env var from ops, like a `.env` next to the file would. So a service that is not built from your repo, such as Postgres, gets its secrets too, and none lands in the repo:

```yaml
services:
  db:
    image: postgres:17
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD}
```

Set it with `ops env set DB_PASSWORD=...`, then deploy. A change to such a variable takes a deploy, not just a restart.

## Set an env var

Adds an env var or changes its value.

```surfaces
Web: the app page, **Environment**, **Set**
CLI: ops env set KEY=value
TUI: on the app, `e`, then type `KEY=value`
API: PATCH /api/v1/apps/{app}/env
MCP: set_env
```

`ops env set` takes several at once: `ops env set KEY1=value KEY2=value`. Put quotes around a value with spaces: `ops env set GREETING="hello world"`. In the API, send them as `set`. Then restart the app: [`ops restart`](/docs/guides/deploy#restart). Until then the app runs with the old values.

## Remove an env var

Deletes an env var from the app.

```surfaces
Web: the app page, **Environment**, the × next to the env var
CLI: ops env rm KEY
TUI: on the app, `e`, type `-KEY`, then `y`
API: PATCH /api/v1/apps/{app}/env
MCP: set_env
```

In the API and MCP, send the names as `unset`.

## Edit them all as one text

Shows every env var as one `.env` file, `KEY=value` per line, so you can paste a whole file. Saving it replaces all env vars: a key that is not in the text is removed. So ops first shows which keys it adds, changes and removes, by name only, and asks.

```surfaces
Web: the app page, **Environment**, switch **Fields** to **Text**, then **Review changes**
CLI: ops env push [file]
TUI: on the app, `e`, then type `edit`: the text opens in `$EDITOR`
API: PUT /api/v1/apps/{app}/env
MCP: replace_env
```

`ops env push` reads `.env` unless you name another file, and `--yes` skips the question. The TUI opens `$VISUAL` or `$EDITOR`, else `vi`, with a file only you can read; ops deletes it as soon as the editor closes. In the API and MCP, send every env var as `env`; the answer names the keys `added`, `changed` and `removed`.

The text takes `#` comments, `export KEY=value`, and values in quotes: in double quotes `\n` is a line break, in single quotes everything stays as it is. Then restart the app, as after every change.

## Use them on your computer

Runs your app locally with the same env vars as on the server:

```bash
ops env pull           # writes .env, readable only by you
ops run -- go run .    # runs a command with the env vars, without a file
```

Commands take the app from `--app`, from `x-ops.app` in [the repo](/docs/guides/config) or from the folder name.
