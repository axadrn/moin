package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/axadrn/moin/protocol"
)

// TableRows is one page of a table. When the table has a rowid, it is the
// first column.
type TableRows struct {
	Database  string           `json:"database"`
	Table     protocol.DBTable `json:"table"`
	Columns   []string         `json:"columns"`
	Rows      [][]any          `json:"rows"`
	Offset    int              `json:"offset"`
	Limit     int              `json:"limit"`
	Truncated bool             `json:"truncated"`
}

// Tables lists the tables of an app's database; "" is the first one.
func (c *Client) Tables(ctx context.Context, app, database string) (protocol.DBTables, error) {
	var tables protocol.DBTables
	err := c.do(ctx, http.MethodGet, appPath(app, "databases/tables")+"?database="+url.QueryEscape(database), nil, &tables)
	return tables, err
}

// Rows returns one page of a table, newest first.
func (c *Client) Rows(ctx context.Context, app, database, table string, offset, limit int) (TableRows, error) {
	query := url.Values{}
	query.Set("database", database)
	query.Set("table", table)
	query.Set("offset", strconv.Itoa(offset))
	query.Set("limit", strconv.Itoa(limit))
	var rows TableRows
	err := c.do(ctx, http.MethodGet, appPath(app, "databases/rows")+"?"+query.Encode(), nil, &rows)
	return rows, err
}

// Query runs SQL on an app's database. Without write it cannot change
// anything.
func (c *Client) Query(ctx context.Context, app, database, sql string, write bool, limit int) (protocol.DBResult, error) {
	body := map[string]any{"database": database, "sql": sql, "write": write, "limit": limit}
	var result protocol.DBResult
	err := c.do(ctx, http.MethodPost, appPath(app, "databases/query"), body, &result)
	return result, err
}
