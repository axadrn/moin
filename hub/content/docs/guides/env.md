---
title: "Env vars"
description: "Encrypted settings and secrets per app"
group: "Apps"
order: 3
---

Env vars are the settings and secrets your app reads when it starts. `DATABASE_URL` or an API key, for example. moin keeps them encrypted. Your app gets them on the next restart, start or deploy.

Values are write-only. moin shows the name and when it changed, never the value. Keep your secrets in a password manager: moin holds them for your server, not for you.

```text
moin env set KEY=value
   │
   ▼
hub            saved encrypted;
   │           the change waits
   │
   │ moin restart, start or deploy
   ▼
daemon         sets them on the
   │           app's services
   ▼
your app       reads KEY when it
               starts
```

They go to the services moin builds and the main one. An image you do not build, such as Postgres, takes them with `${NAME}`: [In compose.yml](#in-composeyml).

## Set an env var

```surfaces
Web: the app, **Environment**, **Set**
CLI: moin env set KEY=value
TUI: on the app, `e`, `a`, then type `KEY=value`
API: PATCH /api/v1/apps/{app}/env
MCP: set_env
```

```bash
moin env set KEY1=value KEY2=value
moin env set GREETING="hello world"
moin restart
```

Saving never restarts the app. Change several, then [restart](/docs/guides/deploy#restart) once. The API takes them as `set`.

## See the env vars

Each name and when it changed. No surface shows a value.

```surfaces
Web: the app's **Settings** tab, **Environment**
CLI: moin env
TUI: on the app, `e`
API: GET /api/v1/apps/{app}/env
MCP: list_env
```

To change a value, set it again.

## Remove an env var

```surfaces
Web: the app's **Settings** tab, **Environment**, × on its row
CLI: moin env rm KEY
TUI: on the app, `e`, pick the key, `x`, then `y`
API: PATCH /api/v1/apps/{app}/env
MCP: set_env
```

The API and MCP take the names as `unset`.

## Edit them all as one text

All env vars as one `.env` file, `KEY=value` per line. Saving replaces all of them. A key missing from the text is removed. The text starts empty: paste your whole `.env`. moin first shows which names it adds, replaces and removes, and asks.

```surfaces
Web: the app, **Environment**, switch **Fields** to **Text**, then **Review changes**
CLI: moin env push [file]
TUI: on the app, `e`, then `E`: an empty text opens in `$EDITOR`
API: PUT /api/v1/apps/{app}/env
MCP: replace_env
```

`moin env push` reads `.env` unless you name a file. `--yes` skips the question. The text takes `#` comments, `export KEY=value` and quoted values. In double quotes `\n` is a line break. In single quotes everything stays as it is.

## In compose.yml

`${NAME}` in `compose.yml` takes the env var from moin. So an image you do not build gets its secrets too, and none lands in the repo:

```yaml
services:
  db:
    image: postgres:17
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD}
```

A change to such a variable takes a deploy, not just a restart.

## Good to know

- Your computer keeps its own `.env` for local work. moin never sends a value back.
- A [catalog](/docs/guides/catalog) app whose first sign-in needs a password moin made shows it once, when you install it.
- An AI agent that sets values over MCP sees them, and so does its model provider. Give it only the secrets it needs.
- A rollback keeps the env vars of its deploy.
- Commands take the app from `--app`, from `x-moin.app` in [the repo](/docs/guides/config), or from the folder name.
