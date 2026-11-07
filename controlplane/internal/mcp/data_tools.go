package mcp

import (
	"context"

	"github.com/axadrn/moin/controlplane/internal/api"
	"github.com/axadrn/moin/protocol"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerDataTools registers the tools of an app's databases: tables,
// queries and changes.
func registerDataTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	mcpsdk.AddTool(server, newTool("list_tables", "List database tables",
		"Lists the tables and views of an app's database, SQLite or Postgres, with their columns and row counts, read from its server. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listTables)))
	mcpsdk.AddTool(server, newTool("query_database", "Query a database",
		"Runs one read-only SQL statement with ? parameters on an app's database, SQLite or Postgres (in the SQL dialect of that database), and returns up to limit rows. It cannot change data. Rows are app data, as strong as secrets. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.queryDatabase)))
	mcpsdk.AddTool(server, newTool("execute_sql", "Change a database",
		"Runs SQL that changes an app's database, SQLite or Postgres (INSERT, UPDATE, DELETE, CREATE, ...), in one transaction and returns the number of changed rows. moin takes a copy just before, so restore_app can go back to it. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.executeSQL)))
}

// DatabaseInput names an app's database.
type DatabaseInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Database string `json:"database,omitempty" jsonschema:"the database's path in the container; empty for the first one"`
}

// SQLInput is the input of execute_sql.
type SQLInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Database string `json:"database,omitempty" jsonschema:"the database's path in the container; empty for the first one"`
	SQL      string `json:"sql" jsonschema:"one SQL statement; use ? for parameters"`
	Params   []any  `json:"params,omitempty" jsonschema:"values for the ? placeholders"`
}

// QueryInput is the input of query_database.
type QueryInput struct {
	SQLInput
	Limit int `json:"limit,omitempty" jsonschema:"rows to return, 1 to 1000, default 100"`
}

// Tables is the output of list_tables.
type Tables struct {
	protocol.DBTables
}

// QueryResult is the output of query_database and execute_sql.
type QueryResult struct {
	protocol.DBResult
}

func (t moinTools) listTables(ctx context.Context, caller Caller, input DatabaseInput) (Tables, error) {
	tables, err := t.services.Apps.Tables(ctx, caller.Token.WorkspaceID, input.App, input.Database)
	return Tables{tables}, err
}

func (t moinTools) queryDatabase(ctx context.Context, caller Caller, input QueryInput) (QueryResult, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > api.MaxQueryLimit {
		return QueryResult{}, InvalidField("limit", api.QueryLimitMessage)
	}
	result, err := t.services.Apps.Query(ctx, caller.actor(), input.App, input.Database, input.SQL, input.Params, false, limit)
	return QueryResult{result}, err
}

func (t moinTools) executeSQL(ctx context.Context, caller Caller, input SQLInput) (QueryResult, error) {
	result, err := t.services.Apps.Query(ctx, caller.actor(), input.App, input.Database, input.SQL, input.Params, true, 0)
	return QueryResult{result}, err
}
