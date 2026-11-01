---
title: "Data"
description: "Browse, edit and query the SQLite database of an app"
order: 6
---

ops reads your app's SQLite database live from its server. You browse tables, edit a cell, add or delete a row and run SQL, from the web, the CLI, the terminal UI, the API and MCP. Nothing is copied to ops: every read goes to the server and back.

ops finds the database on its own: every SQLite file on a volume of the live release, up to three folders deep. Name one with `x-ops.database` when there are several.

| Action | Web | CLI | TUI | MCP |
| --- | --- | --- | --- | --- |
| Tables and row counts | app page, **Data** | `ops db` | `s` | `list_tables` |
| Browse rows | click a table | `ops db rows orders` | `s`, then `SELECT …` | `query_database` |
| Edit a cell | click it, type, `Enter` | `ops sql --write "UPDATE …"` | `s`, then `UPDATE …`, `y` | `execute_sql` |
| Add a row | **+ Row** | `ops sql --write "INSERT …"` | `s`, then `INSERT …`, `y` | `execute_sql` |
| Delete a row | trash icon on the row | `ops sql --write "DELETE …"` | `s`, then `DELETE …`, `y` | `execute_sql` |
| Schema | **Schema** | `ops db schema [table]` | `s`, then `SELECT sql FROM sqlite_schema` | `list_tables` |
| Run SQL | **SQL** box | `ops sql "…"` | `s` | `query_database` |

## Reads cannot change anything

A query opens the database read-only. A statement that would change data fails, so a typo cannot delete rows. To change data, say so:

- Web: tick **Allow changes** in the SQL box. Editing a cell or deleting a row in the table is a change you asked for.
- CLI: `ops sql --write "UPDATE settings SET value = 'on' WHERE key = 'maintenance'"`.
- TUI: run it; ops asks **This query changes data. Run it?** and `y` runs it.
- MCP: `execute_sql`, which agents see as destructive.

Every change runs in one transaction, and with [backups](/docs/guides/backups) set up ops takes a snapshot first. A read returns at most 1000 rows; `--limit` and `limit` choose fewer.

```bash
ops db                              # tables, rows, migration
ops db rows orders --limit 5        # newest rows first
ops sql "SELECT status, count(*) FROM orders GROUP BY status"
echo "SELECT * FROM customers" | ops sql
```

## The schema belongs to your code

ops changes data, never the schema. Tables, columns and indexes change through migrations in your repo, which your app runs when it starts: goose, golang-migrate, Prisma, Drizzle or your own. That keeps the database on the server and the code in the repo the same.

ops shows where the migrations are: the data page, `ops db` and `list_tables` name the tool and its newest version when they find a `goose_db_version`, `schema_migrations`, `_prisma_migrations` or `__drizzle_migrations` table. The **Schema** view shows each table's `CREATE` statement and its indexes.

SQL that creates, alters or drops is still possible for an emergency. The web UI warns you that the next deploy will not know about it.

## Who may read data

Rows are your users' data, as sensitive as secrets. The API and MCP need a token with write access to read rows or run SQL; a read-only token sees the tables and their row counts.

## Requirements

The app keeps SQLite on a volume, as `/data/app.db` in the generated compose file. WAL mode is best: your app and ops then read and write at the same time without waiting. The agent runs as root; after a change it gives the database files back to their owner, so an app that runs as another user keeps working.
