---
title: "Data"
description: "Browse, edit and query the SQLite database of an app"
order: 6
---

ops reads your app's SQLite database live from its server. You can browse tables, edit a cell, add or delete a row and run SQL. Nothing is copied to ops: every read goes to the server and back.

ops finds the database by itself: every SQLite file on a volume of the live release, up to three folders deep. When there are several, name one with `x-ops.database`.

## See the tables

Lists the tables of the database with their row counts.

```surfaces
Web: the app page, **Data** tab
CLI: ops db
TUI: on the app, `s`
API: GET /api/v1/apps/{app}/databases/tables
MCP: list_tables
```

## Browse rows

Shows the rows of one table, newest first.

```surfaces
Web: the app page, **Data** tab, click a table
CLI: ops db rows orders
TUI: on the app, `s`, then type `SELECT * FROM orders`
API: GET /api/v1/apps/{app}/databases/rows
MCP: query_database
```

A read returns at most 1000 rows. `--limit` in the CLI and `limit` in the API and MCP choose fewer; a limit outside 1 to 1000 is refused.

## See the schema

Shows how a table is built: its `CREATE` statement and its indexes.

```surfaces
Web: the app page, **Data** tab, a table, **Schema**
CLI: ops db schema orders
TUI: on the app, `s`, then type `SELECT sql FROM sqlite_schema`
API: GET /api/v1/apps/{app}/databases/tables
MCP: list_tables
```

## Run SQL

Runs a query on the live database. A query only reads, unless you say it may change data.

```surfaces
Web: the app page, **Data** tab, the **SQL** box, **Run**
CLI: ops sql "SELECT status, count(*) FROM orders GROUP BY status"
TUI: on the app, `s`, then type the query
API: POST /api/v1/apps/{app}/databases/query
MCP: query_database
```

## Change data

Edits a cell, adds a row or deletes one. With [backups](/docs/guides/backups) set up, ops takes a snapshot first.

```surfaces
Web: the app page, **Data** tab: click a cell, type, then `Enter`; **Add row**; or the trash icon on a row
Web: or tick **Allow changes** in the **SQL** box
CLI: ops sql --write "UPDATE settings SET value = 'on' WHERE key = 'maintenance'"
TUI: on the app, `s`, type the query, then `y` when ops asks **This query changes data. Run it?**
API: PATCH /api/v1/apps/{app}/databases/rows
MCP: execute_sql
```

The API adds a row with `POST` and deletes one with `DELETE` on the same path, and runs SQL that changes data through `/databases/query`. Agents see `execute_sql` as destructive, so they ask first.

## Reads cannot change anything

A query opens the database read-only. A statement that would change data fails, so a typo cannot delete rows. To change data you say so, as above. Every change runs in one transaction.

More from the CLI:

```bash
ops db                              # tables, rows, migration
ops db rows orders --limit 5        # newest rows first
echo "SELECT * FROM customers" | ops sql
```

## The schema belongs to your code

ops changes data, never the schema. Tables, columns and indexes change through migrations in your repo, which your app runs when it starts: goose, golang-migrate, Prisma, Drizzle or your own. That keeps the database on the server and the code in the repo the same.

ops shows where the migrations are. The data page, `ops db` and `list_tables` name the tool and its newest version when they find a `goose_db_version`, `schema_migrations`, `_prisma_migrations` or `__drizzle_migrations` table.

SQL that creates, alters or drops is still possible for an emergency. The web warns you that the next deploy will not know about it.

## Who may read data

Rows are your users' data, as sensitive as secrets. The API and MCP need a token with write access to read rows or run SQL. A read-only token sees the tables and their row counts.

## Requirements

The app keeps SQLite on a volume, as `/data/app.db` in the compose file ops writes. WAL mode is best: your app and ops then read and write at the same time without waiting. The agent runs as root; after a change it gives the database files back to their owner, so an app that runs as another user keeps working.
