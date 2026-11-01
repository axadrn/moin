package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/protocol"
)

const dataTimeout = 40 * time.Second

var (
	ErrNoDatabase = errors.New("the live release has no SQLite database on a volume")
	ErrNoTable    = errors.New("the database has no such table")
	ErrNoRowID    = errors.New("rows of views and WITHOUT ROWID tables cannot be edited here: use SQL")
	ErrNoColumn   = errors.New("the table has no such column")
	ErrSQL        = errors.New("the query is empty")
)

// Rows is one page of a table: its rows with the rowid first when the
// table has one.
type Rows struct {
	Table  protocol.DBTable
	Result protocol.DBResult
	Offset int
	Limit  int
}

// Databases returns the SQLite databases of the app's live release, as the
// agent found them.
func (s *AppService) Databases(workspaceID, idOrName string) (*model.App, []protocol.Database, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	if app.LiveNumber == 0 {
		return app, []protocol.Database{}, nil
	}
	deploy, err := s.apps.Deploy(app.ID, app.LiveNumber)
	if err != nil {
		return nil, nil, err
	}
	detected := Detected(*deploy)
	if detected == nil || detected.Databases == nil {
		return app, []protocol.Database{}, nil
	}
	return app, detected.Databases, nil
}

// Tables asks the agent for the tables of one of the app's databases. An
// empty database means the first one.
func (s *AppService) Tables(ctx context.Context, workspaceID, idOrName, database string) (protocol.DBTables, error) {
	app, databases, err := s.Databases(workspaceID, idOrName)
	if err != nil {
		return protocol.DBTables{}, err
	}
	if len(databases) == 0 {
		return protocol.DBTables{}, ErrNoDatabase
	}
	ctx, cancel := context.WithTimeout(ctx, dataTimeout)
	defer cancel()
	var tables protocol.DBTables
	err = s.servers.Request(ctx, app.ServerID, protocol.TypeDBTables, protocol.DBRequest{App: app.Name, Database: database}, &tables)
	return tables, err
}

// Query runs SQL on one of the app's databases. A read returns at most
// limit rows and cannot change anything; a write runs in one transaction
// and goes into the activity with what it did and how many rows it changed,
// such as "UPDATE on orders", never its SQL: that may hold your users' data.
func (s *AppService) Query(ctx context.Context, actor Actor, idOrName, database, sql string, params []any, write bool, limit int) (protocol.DBResult, error) {
	if write {
		if err := s.servers.RequireChanges(actor.WorkspaceID); err != nil {
			return protocol.DBResult{}, err
		}
	}
	result, err := s.query(ctx, actor.WorkspaceID, idOrName, database, sql, params, write, limit)
	if err == nil && write {
		s.servers.record(actor, "data.sql", "app", idOrName, map[string]string{"database": database, "statements": sqlSummary(sql), "changes": strconv.FormatInt(result.Changes, 10)})
	}
	return result, err
}

func (s *AppService) query(ctx context.Context, workspaceID, idOrName, database, sql string, params []any, write bool, limit int) (protocol.DBResult, error) {
	if strings.TrimSpace(sql) == "" {
		return protocol.DBResult{}, ErrSQL
	}
	app, databases, err := s.Databases(workspaceID, idOrName)
	if err != nil {
		return protocol.DBResult{}, err
	}
	if len(databases) == 0 {
		return protocol.DBResult{}, ErrNoDatabase
	}
	ctx, cancel := context.WithTimeout(ctx, dataTimeout)
	defer cancel()
	query := protocol.DBQuery{
		DBRequest: protocol.DBRequest{App: app.Name, Database: database},
		SQL:       sql,
		Params:    params,
		Write:     write,
		Limit:     limit,
		Snapshot:  write && s.snapshotBeforeWrite(app),
	}
	var result protocol.DBResult
	err = s.servers.Request(ctx, app.ServerID, protocol.TypeDBQuery, query, &result)
	return result, err
}

// Rows returns one page of a table, newest rowid first.
func (s *AppService) Rows(ctx context.Context, workspaceID, idOrName, database, table string, offset, limit int) (Rows, error) {
	tables, err := s.Tables(ctx, workspaceID, idOrName, database)
	if err != nil {
		return Rows{}, err
	}
	index := slices.IndexFunc(tables.Tables, func(t protocol.DBTable) bool { return t.Name == table })
	if index < 0 {
		return Rows{}, ErrNoTable
	}
	page := Rows{Table: tables.Tables[index], Offset: max(offset, 0), Limit: min(max(limit, 1), 500)}
	sql := `SELECT * FROM ` + quoteName(table) + ` LIMIT ? OFFSET ?`
	if page.Table.RowID {
		sql = `SELECT rowid, * FROM ` + quoteName(table) + ` ORDER BY rowid DESC LIMIT ? OFFSET ?`
	}
	page.Result, err = s.query(ctx, workspaceID, idOrName, tables.Database, sql, []any{page.Limit, page.Offset}, false, page.Limit)
	return page, err
}

// UpdateCell sets one column of one row. A nil value stores NULL.
func (s *AppService) UpdateCell(ctx context.Context, actor Actor, idOrName, database, table string, rowID int64, column string, value *string) error {
	target, err := s.editable(ctx, actor.WorkspaceID, idOrName, database, table)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(target.Columns, func(c protocol.DBColumn) bool { return c.Name == column }) {
		return ErrNoColumn
	}
	var param any
	if value != nil {
		param = *value
	}
	_, err = s.query(ctx, actor.WorkspaceID, idOrName, database, `UPDATE `+quoteName(table)+` SET `+quoteName(column)+` = ? WHERE rowid = ?`, []any{param, rowID}, true, 0)
	if err == nil {
		s.servers.record(actor, "data.cell_updated", "app", idOrName, map[string]string{"database": database, "table": table, "row": strconv.FormatInt(rowID, 10), "column": column})
	}
	return err
}

// DeleteRow deletes one row by its rowid.
func (s *AppService) DeleteRow(ctx context.Context, actor Actor, idOrName, database, table string, rowID int64) error {
	_, err := s.editable(ctx, actor.WorkspaceID, idOrName, database, table)
	if err != nil {
		return err
	}
	_, err = s.query(ctx, actor.WorkspaceID, idOrName, database, `DELETE FROM `+quoteName(table)+` WHERE rowid = ?`, []any{rowID}, true, 0)
	if err == nil {
		s.servers.record(actor, "data.row_deleted", "app", idOrName, map[string]string{"database": database, "table": table, "row": strconv.FormatInt(rowID, 10)})
	}
	return err
}

func (s *AppService) editable(ctx context.Context, workspaceID, idOrName, database, table string) (protocol.DBTable, error) {
	tables, err := s.Tables(ctx, workspaceID, idOrName, database)
	if err != nil {
		return protocol.DBTable{}, err
	}
	index := slices.IndexFunc(tables.Tables, func(t protocol.DBTable) bool { return t.Name == table })
	if index < 0 {
		return protocol.DBTable{}, ErrNoTable
	}
	if !tables.Tables[index].RowID {
		return protocol.DBTable{}, ErrNoRowID
	}
	return tables.Tables[index], nil
}

func quoteName(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// InsertRow adds a row to a table. Columns missing from values get their
// default.
func (s *AppService) InsertRow(ctx context.Context, actor Actor, idOrName, database, table string, values map[string]string) error {
	tables, err := s.Tables(ctx, actor.WorkspaceID, idOrName, database)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(tables.Tables, func(t protocol.DBTable) bool { return t.Name == table && t.Type == "table" })
	if index < 0 {
		return ErrNoTable
	}
	sql, params, err := insertSQL(tables.Tables[index], values)
	if err != nil {
		return err
	}
	_, err = s.query(ctx, actor.WorkspaceID, idOrName, database, sql, params, true, 0)
	if err == nil {
		s.servers.record(actor, "data.row_inserted", "app", idOrName, map[string]string{"database": database, "table": table})
	}
	return err
}

// insertSQL is the INSERT for values, in the table's column order.
func insertSQL(table protocol.DBTable, values map[string]string) (string, []any, error) {
	names := []string{}
	marks := []string{}
	params := []any{}
	for column := range values {
		if !slices.ContainsFunc(table.Columns, func(c protocol.DBColumn) bool { return c.Name == column }) {
			return "", nil, ErrNoColumn
		}
	}
	for _, column := range table.Columns {
		value, ok := values[column.Name]
		if !ok {
			continue
		}
		names = append(names, quoteName(column.Name))
		marks = append(marks, "?")
		params = append(params, value)
	}
	if len(names) == 0 {
		return `INSERT INTO ` + quoteName(table.Name) + ` DEFAULT VALUES`, nil, nil
	}
	return `INSERT INTO ` + quoteName(table.Name) + ` (` + strings.Join(names, ", ") + `) VALUES (` + strings.Join(marks, ", ") + `)`, params, nil
}
