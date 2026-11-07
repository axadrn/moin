---
title: "Data"
description: "Browse, edit and query the database of an app, SQLite or Postgres"
group: "Every day"
order: 4
---

Browse the tables of your app's live database, edit a cell, add or delete a row and run SQL, on SQLite or Postgres. Every read goes to the server and back; nothing is copied to moin.

## See the tables

```surfaces
Web: the app, **Data**
CLI: moin db
TUI: opens the web: `w`, then `d`
API: GET /api/v1/apps/{app}/databases/tables
MCP: list_tables
```

Tables with their row counts, and the migration tool with its newest version when moin finds one.

## Browse rows

Newest first, at most 1000 rows. `--limit` (CLI) or `limit` (API, MCP) chooses fewer; outside 1 to 1000 it is refused.

```surfaces
Web: the app, **Data**, click a table
CLI: moin db rows <table>
TUI: opens the web: `w`, then `d`
API: GET /api/v1/apps/{app}/databases/rows
MCP: query_database
```

## See the schema

A table's `CREATE` statement and its indexes.

```surfaces
Web: the app, **Data**, a table, **Schema**
CLI: moin db schema <table>
TUI: opens the web: `w`, then `d`
API: GET /api/v1/apps/{app}/databases/tables
MCP: list_tables
```

## Run SQL

A query only reads, unless you say it may change data.

```surfaces
Web: the app, **Data**, the **SQL** box, **Run**
CLI: moin sql "SELECT status, count(*) FROM orders GROUP BY status"
TUI: opens the web: `w`, then `d`
API: POST /api/v1/apps/{app}/databases/query
MCP: query_database
```

```bash
moin db rows orders --limit 5
echo "SELECT * FROM customers" | moin sql
```

## Change data

Edit a cell, add a row or delete one. With [backups](/docs/guides/backups) set up, moin takes a full backup first, so one restore undoes it.

```surfaces
Web: the app, **Data**: click a cell, type, then `Enter`; **Add row**; or the trash icon on a row
Web: or tick **Allow changes** in the **SQL** box
CLI: moin sql --write "UPDATE settings SET value = 'on' WHERE key = 'maintenance'"
TUI: opens the web: `w`, then `d`
API: PATCH /api/v1/apps/{app}/databases/rows
MCP: execute_sql
```

The API adds a row with `POST` and deletes one with `DELETE` on the same path, and runs SQL that changes data through `/databases/query`. It names a row by `rowid` on SQLite, or by `key`, every column of the primary key, such as `{"id": "42"}`. Agents see `execute_sql` as destructive, so they ask first.

## The schema belongs to your code

moin changes data, not tables, columns or indexes: those change through the migrations your app runs when it starts (goose, golang-migrate, Prisma, Drizzle or your own). SQL that creates, alters or drops still works for an emergency; the web warns that the next deploy will not know about it.

## Good to know

- **Reads cannot change anything.** A read opens the database read-only (on Postgres a `READ ONLY` transaction, one statement), so a typo cannot delete rows. Every change runs in one transaction.
- **Which database.** moin finds every SQLite file on a volume of the live deploy, up to three folders deep, and every Postgres service, named `postgres-<service>`. With several, set `x-moin.database` in [the repo](/docs/guides/config), or `--db <path>` / `--db postgres-db` in the CLI.
- **Rows by key.** moin edits a row by its rowid on SQLite and by its primary key on Postgres. A table without a primary key takes new rows; change them with SQL.
- **Who may read.** Rows are your users' data: the API and MCP need a token with write access to read rows or run SQL. A read-only token sees the tables and row counts.
- **Off on a server.** `data: off` in the [rules](/docs/guides/security#rules) refuses reading and changing data through moin there.
- More on how reads and Postgres work: [How moin reads your database](/docs/concepts/how-moin-works#how-moin-reads-your-database).

## Requirements

- SQLite on a volume, such as `/data/app.db` in the compose file moin writes. WAL mode is best: your app turns it on with `PRAGMA journal_mode=WAL`.
- Postgres from the official image or one built on it (`postgres`, `postgis/postgis`, `pgvector/pgvector`, `timescale/timescaledb`), running. moin shows the tables of the `public` schema.
- The data page says the app has no database? The live deploy keeps no SQLite file on a volume and runs no Postgres: put the file in a mounted folder, such as `/data`, and deploy again.
