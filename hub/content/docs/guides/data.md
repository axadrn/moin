---
title: "Data"
description: "Browse, edit and query the database of an app, SQLite or Postgres"
group: "Every day"
order: 4
---

See the tables of your app's live database. Edit a cell, add or delete a row, or run SQL. It works on SQLite and Postgres.

Every read goes to the server and back. Nothing is copied to moin.

## See the tables

```surfaces
Web: the app page, **Data**, a database
CLI: moin db
TUI: opens the web: `w`, then `d`
API: GET /api/v1/apps/{app}/databases/tables
MCP: list_tables
```

## Browse rows

Newest first, at most 1000 rows. `--limit` (CLI) or `limit` (API, MCP) chooses fewer.

```surfaces
Web: the app page, **Data**, a database, a table
CLI: moin db rows <table>
TUI: opens the web: `w`, then `d`
API: GET /api/v1/apps/{app}/databases/rows
MCP: query_database
```

## See the schema

A table's `CREATE` statement and its indexes.

```surfaces
Web: the app page, **Data**, a database, a table: **Columns** and **Statement** under the rows
CLI: moin db schema <table>
TUI: opens the web: `w`, then `d`
API: GET /api/v1/apps/{app}/databases/tables
MCP: list_tables
```

## Run SQL

A query only reads, unless you say it may change data.

```surfaces
Web: the app page, **Data**, a database, **SQL**, **Run**
CLI: moin sql "SELECT plan, count(*) FROM users GROUP BY plan"
TUI: opens the web: `w`, then `d`
API: POST /api/v1/apps/{app}/databases/query
MCP: query_database
```

```bash
moin db rows users --limit 5
echo "SELECT * FROM customers" | moin sql
```

## Change data

Edit a cell, add a row or delete one. moin takes a copy right before, so [Backups](/docs/guides/backups) offers "Before your edit" to go back to.

A database over 1 GB gets no extra copy before an edit. The newest copy from your schedule is the way back.

```surfaces
Web: the app page, **Data**, a database, a table: click a cell, type, then `Enter`; **Add row**; or the trash icon on a row
Web: or turn on **Allow changes** under **SQL**
CLI: moin sql --write "UPDATE settings SET value = 'on' WHERE key = 'maintenance'"
TUI: opens the web: `w`, then `d`
API: PATCH /api/v1/apps/{app}/databases/rows
MCP: execute_sql
```

The API adds a row with `POST`, deletes one with `DELETE` on the same path, and runs SQL that changes data through `/databases/query`. It names a row by `rowid` on SQLite, or by `key`, every column of the primary key, on Postgres.

## The schema belongs to your code

moin changes data, not tables or columns. Those change through the migrations your app runs when it starts. SQL that creates, alters or drops still works for an emergency, but the next deploy will not know about it.

## Good to know

- **Reads cannot change anything.** A read opens the database read-only. A typo cannot delete rows.
- **Which database.** With several, pick one in the list, or use `--db <path>` or `--db postgres-db` in the CLI.
- **No primary key?** A table without one takes new rows. Change them with SQL.
- **Who may read.** Rows are your users' data. The API and MCP need a token with write access to read rows or run SQL.
- **Off on a server.** `data: off` in the [rules](/docs/guides/security#rules) refuses reading and changing data through moin there.

## Requirements

- SQLite in a folder or volume of the app, such as `/data/app.db`. WAL mode is best: your app turns it on with `PRAGMA journal_mode=WAL`.
- Postgres from the official image or one built on it (`postgres`, `postgis/postgis`, `pgvector/pgvector`, `timescale/timescaledb`), running. moin shows the tables of the `public` schema.
- No database shows? Put the file in a mounted folder, such as `/data`, and deploy again. Or wait: every hour moin [looks for new databases](/docs/guides/backups#what-moin-backs-up).
