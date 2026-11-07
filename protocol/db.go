package protocol

import "time"

// DBRequest names an app's database by its Path in Detected.Databases: a
// SQLite file in the container, or postgres-<service>. An empty Database
// means the first one.
type DBRequest struct {
	App      string `json:"app"`
	Database string `json:"database,omitempty"`
}

// DBTables lists the tables and views of a database, and the newest
// migration a known migration tool recorded in it.
type DBTables struct {
	Database  string       `json:"database"`
	Tables    []DBTable    `json:"tables"`
	Migration *DBMigration `json:"migration,omitempty"`
}

// DBMigration is the newest migration of goose, golang-migrate, Prisma or
// Drizzle. The app runs its migrations; moin only reads where they are.
type DBMigration struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	AppliedAt string `json:"applied_at,omitempty"`
}

// DBTable is one table or view. RowID is false for views, WITHOUT ROWID
// tables and every Postgres table: their rows cannot be edited by rowid.
type DBTable struct {
	Name    string     `json:"name"`
	Type    string     `json:"type"`
	Rows    int64      `json:"rows"`
	RowID   bool       `json:"rowid"`
	Columns []DBColumn `json:"columns"`
	// SQL is the CREATE statement, Indexes the CREATE INDEX statements.
	SQL     string   `json:"sql,omitempty"`
	Indexes []string `json:"indexes,omitempty"`
}

// DBColumn is one column of a table.
type DBColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null,omitempty"`
	PrimaryKey bool   `json:"primary_key,omitempty"`
}

// DBQuery runs SQL with positional parameters. A read runs read-only and
// returns at most Limit rows. A write runs in one transaction and returns
// the number of changed rows.
type DBQuery struct {
	DBRequest
	SQL    string `json:"sql"`
	Params []any  `json:"params,omitempty"`
	Write  bool   `json:"write,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// DBResult is the answer to a DBQuery. Values are null, numbers or text;
// a blob that is not text comes as "0x" and its hex. Truncated is true when
// the query had more rows than the limit.
type DBResult struct {
	Database  string   `json:"database"`
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated,omitempty"`
	Changes   int64    `json:"changes,omitempty"`
	// Before is when the daemon took the copy just before a write, the
	// time in its ID: the hub names it "Before your edit".
	Before *time.Time `json:"before,omitempty"`
}
