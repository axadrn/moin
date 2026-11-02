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

Reading values needs an API token with write access: reading a secret is as strong as writing it.

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

## Use them on your computer

Runs your app locally with the same env vars as on the server:

```bash
ops env pull           # writes .env, readable only by you
ops run -- go run .    # runs a command with the env vars, without a file
```

Commands take the app from `--app`, from `x-ops.app` in [the repo](/docs/guides/config) or from the folder name.
