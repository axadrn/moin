package agent

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/axadrn/ops/protocol"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	defaultRows   = 100
	maxRows       = 1000
	maxResultSize = 2 << 20
	queryTimeout  = 30 * time.Second
)

// database returns the database of the running release that request names,
// or its first one.
func (a *apps) database(request protocol.DBRequest) (protocol.Database, error) {
	if !protocol.ValidAppName(request.App) {
		return protocol.Database{}, errors.New("invalid app name")
	}
	current := a.current(request.App)
	if current == 0 {
		return protocol.Database{}, fmt.Errorf("no release of %s runs on this server", request.App)
	}
	var detected protocol.Detected
	body, _ := os.ReadFile(filepath.Join(a.releaseDir(request.App, current), "detected.json"))
	_ = json.Unmarshal(body, &detected)
	for _, database := range detected.Databases {
		if request.Database == "" || request.Database == database.Path {
			return database, nil
		}
	}
	if request.Database == "" {
		return protocol.Database{}, fmt.Errorf("no SQLite database on a volume of %s", request.App)
	}
	return protocol.Database{}, fmt.Errorf("no database %s in %s", request.Database, request.App)
}

// openDatabase opens a database file on one connection. A read-only handle
// cannot change anything, not even by a statement that tries. Neither handle
// can attach another file, so SQL reaches only this database: ATTACH and
// VACUUM INTO would otherwise read and write any path as root. close closes
// the connection and the database.
func openDatabase(ctx context.Context, path string, write bool) (conn *sql.Conn, close func(), err error) {
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	if !write {
		query.Add("mode", "ro")
		query.Add("_pragma", "query_only(1)")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+query.Encode())
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err = db.Conn(ctx)
	if err == nil {
		_, err = sqlite.Limit(conn, sqlite3.SQLITE_LIMIT_ATTACHED, 0)
	}
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		db.Close()
		return nil, nil, err
	}
	return conn, func() { conn.Close(); db.Close() }, nil
}

// matchOwner gives the -wal and -shm files of a database the owner of the
// database file. The agent runs as root; an app that runs as another user
// could not open files root created.
func matchOwner(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	matchOwnerOf(path, info)
}

// matchOwnerOf gives the database at path and its -wal and -shm files the
// owner in info.
func matchOwnerOf(path string, info os.FileInfo) {
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Lchown(path+suffix, int(owner.Uid), int(owner.Gid))
	}
}

func quoteName(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// tables lists the tables and views of a database with their columns and
// row counts.
func (a *apps) tables(ctx context.Context, request protocol.DBRequest) (protocol.DBTables, error) {
	database, err := a.database(request)
	if err != nil {
		return protocol.DBTables{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	db, closeDB, err := openDatabase(ctx, database.HostPath, false)
	if err != nil {
		return protocol.DBTables{}, err
	}
	defer closeDB()

	result := protocol.DBTables{Database: database.Path, Tables: []protocol.DBTable{}}
	// Litestream keeps its own _litestream_seq and _litestream_lock tables
	// in the database; they are not the app's.
	rows, err := db.QueryContext(ctx, `SELECT name, type, sql FROM sqlite_schema WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '\_litestream\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var table protocol.DBTable
		var definition sql.NullString
		err = rows.Scan(&table.Name, &table.Type, &definition)
		if err != nil {
			rows.Close()
			return result, err
		}
		table.SQL = definition.String
		table.RowID = table.Type == "table" && !strings.Contains(strings.ToUpper(definition.String), "WITHOUT ROWID")
		result.Tables = append(result.Tables, table)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM `+quoteName(table.Name)).Scan(&table.Rows)
		if err != nil {
			return result, fmt.Errorf("count %s: %w", table.Name, err)
		}
		indexes, err := db.QueryContext(ctx, `SELECT sql FROM sqlite_schema WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL ORDER BY name`, table.Name)
		if err != nil {
			return result, err
		}
		for indexes.Next() {
			var definition string
			if err = indexes.Scan(&definition); err != nil {
				indexes.Close()
				return result, err
			}
			table.Indexes = append(table.Indexes, definition)
		}
		indexes.Close()
		columns, err := db.QueryContext(ctx, `SELECT name, type, "notnull", pk FROM pragma_table_info(?)`, table.Name)
		if err != nil {
			return result, err
		}
		for columns.Next() {
			var column protocol.DBColumn
			var primaryKey int
			err = columns.Scan(&column.Name, &column.Type, &column.NotNull, &primaryKey)
			if err != nil {
				columns.Close()
				return result, err
			}
			column.PrimaryKey = primaryKey > 0
			table.Columns = append(table.Columns, column)
		}
		columns.Close()
	}
	result.Migration = migration(ctx, db, result.Tables)
	return result, nil
}

// migrationTools are the tables migration tools keep, with the query for
// their newest migration: its version and when it ran.
var migrationTools = []struct{ table, tool, query string }{
	{"goose_db_version", "goose", `SELECT version_id, tstamp FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1`},
	{"schema_migrations", "migrate", `SELECT version, '' FROM schema_migrations ORDER BY version DESC LIMIT 1`},
	{"_prisma_migrations", "prisma", `SELECT migration_name, finished_at FROM _prisma_migrations WHERE finished_at IS NOT NULL ORDER BY finished_at DESC LIMIT 1`},
	{"__drizzle_migrations", "drizzle", `SELECT hash, created_at FROM __drizzle_migrations ORDER BY created_at DESC LIMIT 1`},
}

// migration reads the newest migration of the first migration tool whose
// table the database has.
func migration(ctx context.Context, db *sql.Conn, tables []protocol.DBTable) *protocol.DBMigration {
	for _, known := range migrationTools {
		for _, table := range tables {
			if table.Name != known.table {
				continue
			}
			var version, appliedAt any
			if db.QueryRowContext(ctx, known.query).Scan(&version, &appliedAt) != nil {
				return nil
			}
			return &protocol.DBMigration{Tool: known.tool, Version: fmt.Sprint(plain(version)), AppliedAt: strings.TrimSpace(fmt.Sprint(plain(appliedAt)))}
		}
	}
	return nil
}

// query runs one read or one write against a database.
func (a *apps) query(ctx context.Context, request protocol.DBQuery) (protocol.DBResult, error) {
	database, err := a.database(request.DBRequest)
	if err != nil {
		return protocol.DBResult{}, err
	}
	if strings.TrimSpace(request.SQL) == "" {
		return protocol.DBResult{}, errors.New("the query is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	db, closeDB, err := openDatabase(ctx, database.HostPath, request.Write)
	if err != nil {
		return protocol.DBResult{}, err
	}
	defer closeDB()
	result := protocol.DBResult{Database: database.Path, Columns: []string{}, Rows: [][]any{}}

	if request.Write {
		if request.Snapshot && a.backups != nil {
			err = a.backups.snapshotBefore(ctx, request.App, "before-sql", nil)
			if err != nil {
				return result, err
			}
		}
		defer matchOwner(database.HostPath)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return result, err
		}
		defer tx.Rollback()
		done, err := tx.ExecContext(ctx, request.SQL, request.Params...)
		if err != nil {
			return result, err
		}
		result.Changes, _ = done.RowsAffected()
		return result, tx.Commit()
	}

	limit := request.Limit
	if limit <= 0 {
		limit = defaultRows
	}
	limit = min(limit, maxRows)
	rows, err := db.QueryContext(ctx, request.SQL, request.Params...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Columns, err = rows.Columns()
	if err != nil {
		return result, err
	}
	size := 0
	for rows.Next() {
		if len(result.Rows) == limit || size > maxResultSize {
			result.Truncated = true
			break
		}
		values := make([]any, len(result.Columns))
		pointers := make([]any, len(values))
		for index := range values {
			pointers[index] = &values[index]
		}
		err = rows.Scan(pointers...)
		if err != nil {
			return result, err
		}
		for index, value := range values {
			values[index] = plain(value)
			if text, ok := values[index].(string); ok {
				size += len(text)
			}
			size += 8
		}
		result.Rows = append(result.Rows, values)
	}
	return result, rows.Err()
}

// plain turns a SQLite value into one JSON keeps as it is.
func plain(value any) any {
	switch value := value.(type) {
	case []byte:
		if utf8.Valid(value) {
			return string(value)
		}
		return "0x" + hex.EncodeToString(value)
	case time.Time:
		return value.Format(time.RFC3339Nano)
	default:
		return value
	}
}
