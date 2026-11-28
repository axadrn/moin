package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// `moin sql` asks a database of an app one thing and shows the rows.
//
// It reads only, unless you say --write: SQLite is opened read-only by
// moin's own SQLite, Postgres and MySQL get a session that starts
// read-only, from the client in their own container. That stops a slip,
// not you: the session is yours.
//
// A write first takes a copy of the database, the way `moin backup`
// does, and notes it as before-sql. Then the statement runs in one
// transaction. `moin restore <app> --to before-sql` goes back to the copy.

// SQL is one `moin sql`. Database names the database, "" for an app that
// has one. Tables asks for the tables instead of running Query. Limit is
// how many rows come back at most. Write lets the statement change the
// data; NoCopy writes without the copy before it.
type SQL struct {
	App      string
	Database string
	Query    string
	Tables   bool
	Limit    int
	Write    bool
	NoCopy   bool
}

// SQLResult is what a query answered: the columns and the rows, every
// value as text and nil for NULL. More says the query had more rows than
// the limit.
//
// A write answers Affected, how many rows it changed, and Copy, the copy
// taken before it.
//
// Choose are the app's databases when it has several and you named none:
// nothing ran.
type SQLResult struct {
	Database string      `json:"database,omitempty"`
	Columns  []string    `json:"columns"`
	Rows     [][]*string `json:"rows"`
	More     bool        `json:"more,omitempty"`
	Affected int64       `json:"rows_affected"`
	Copy     string      `json:"copy,omitempty"`
	Choose   []string    `json:"choose,omitempty"`
}

const (
	// SQLLimit is how many rows a query answers unless you say --limit,
	// sqlMaxLimit the most it can.
	SQLLimit    = 200
	sqlMaxLimit = 10000
	// sqlMaxBytes is how much a client in a container may answer: a row
	// can be big, and all of it goes through the server's memory.
	sqlMaxBytes = 16 << 20
	// BeforeSQL names the copy taken before the last `moin sql --write`
	// of an app: restore --to before-sql.
	BeforeSQL = "before-sql"
)

// SQLHere runs one `moin sql` on this server.
func SQLHere(ctx context.Context, dir string, request SQL, out io.Writer) (SQLResult, error) {
	return newApps(dir).sql(ctx, request, out)
}

func (a *apps) sql(ctx context.Context, request SQL, out io.Writer) (SQLResult, error) {
	if err := a.mustHave(request.App); err != nil {
		return SQLResult{}, err
	}
	b := a.backups
	database, choose, err := a.sqlDatabase(request.App, request.Database)
	if err != nil || len(choose) > 0 {
		return SQLResult{Choose: choose}, err
	}
	if request.Tables {
		request.Query, request.Write = tablesQuery(database), false
	}
	if strings.TrimSpace(request.Query) == "" {
		return SQLResult{}, errors.New("no query")
	}
	if request.Limit <= 0 {
		request.Limit = SQLLimit
	}
	request.Limit = min(request.Limit, sqlMaxLimit)

	var result SQLResult
	if !request.Write {
		result, err = a.sqlRead(ctx, request.App, database, request.Query, request.Limit)
		result.Database = database.Path
		if err != nil && readOnly.MatchString(err.Error()) {
			err = fmt.Errorf("%w. `%s sql` only reads: to change data, add --write", err, build.Name)
		}
		return result, err
	}
	if !request.NoCopy {
		result.Copy, err = b.copyBeforeSQL(ctx, request.App, database, out)
		if err != nil {
			return SQLResult{}, err
		}
	}
	fmt.Fprintf(out, "running the statement\n")
	result.Affected, err = a.sqlWrite(ctx, request.App, database, request.Query)
	result.Database, result.Columns, result.Rows = database.Path, []string{}, [][]*string{}
	if err != nil {
		return SQLResult{}, fmt.Errorf("%w. The transaction was rolled back", err)
	}
	return result, nil
}

// readOnly is how SQLite, Postgres and MySQL say a read tried to write.
var readOnly = regexp.MustCompile(`(?i)read.?only`)

// speaksSQL reports whether `moin sql` can ask the database: a SQLite
// file, a Postgres, a MySQL or a MariaDB.
func speaksSQL(database protocol.Database) bool {
	return database.Kind == "" || database.Postgres() || database.MySQL()
}

// sqlDatabase is the database of the app that name names, like
// postgres-db, or postgres-db/other for another database of that
// Postgres. Without a name it is the app's only one, and with several
// it answers their names to choose from.
func (a *apps) sqlDatabase(app, name string) (database protocol.Database, choose []string, err error) {
	if name != "" {
		database, err = a.backups.one(app, name)
		if err == nil && !speaksSQL(database) {
			err = fmt.Errorf("%s is no SQLite, Postgres, MySQL or MariaDB database", name)
		}
		return database, nil, err
	}
	var speaking []protocol.Database
	for _, each := range a.liveDatabases(app) {
		if speaksSQL(each) {
			speaking = append(speaking, each)
			choose = append(choose, each.Path)
		}
	}
	switch len(speaking) {
	case 0:
		return protocol.Database{}, nil, fmt.Errorf("%s has no SQLite, Postgres, MySQL or MariaDB database %s knows of", app, build.Name)
	case 1:
		return speaking[0], nil, nil
	}
	return protocol.Database{}, choose, nil
}

// tablesQuery is the query that lists the tables of a database.
func tablesQuery(database protocol.Database) string {
	switch {
	case database.Postgres():
		return `SELECT table_schema AS "schema", table_name AS "table" FROM information_schema.tables WHERE table_type = 'BASE TABLE' AND table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY 1, 2`
	case database.MySQL():
		return "SELECT table_name AS `table` FROM information_schema.tables WHERE table_type = 'BASE TABLE' AND table_schema = DATABASE() ORDER BY 1"
	}
	return `SELECT name AS "table" FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY 1`
}

// sqlRead runs a query that only reads and answers at most limit rows.
func (a *apps) sqlRead(ctx context.Context, app string, database protocol.Database, query string, limit int) (SQLResult, error) {
	switch {
	case database.Postgres():
		t, err := (&postgresEngine{b: a.backups, app: app, database: database}).target(ctx)
		if err != nil {
			return SQLResult{}, err
		}
		// One row more than the limit says there are more. FETCH_COUNT
		// has psql fetch the rows in such steps, not all at once.
		out, cut, err := runSQL(ctx, query, t.container, []string{"PGOPTIONS=-c default_transaction_read_only=on"},
			"psql", "-X", "-q", "-A", "-F", pgField, "-R", pgRow, "-P", "null="+pgNull, "-P", "footer=off", "-v", "ON_ERROR_STOP=1", "-v", "FETCH_COUNT="+strconv.Itoa(limit+1), "-h", pgSocket, "-U", t.user, "-d", t.database)
		return readPsql(out, limit, cut), err
	case database.MySQL():
		t, err := (&mysqlEngine{b: a.backups, app: app, database: database}).target(ctx)
		if err != nil {
			return SQLResult{}, err
		}
		out, cut, err := runSQL(ctx, query, t.container, nil,
			"sh", "-c", mysqlTool, "sh", mysqlClient[0], mysqlClient[1], "-uroot", "--xml", "--quick", "--init-command=SET SESSION TRANSACTION READ ONLY", t.database)
		if err != nil {
			return SQLResult{}, err
		}
		return readXML(out, limit, cut)
	}
	return sqliteRead(ctx, database, query, limit)
}

// sqlWrite runs a statement that may change the data, in one
// transaction, and says how many rows it changed.
func (a *apps) sqlWrite(ctx context.Context, app string, database protocol.Database, statement string) (int64, error) {
	switch {
	case database.Postgres():
		t, err := (&postgresEngine{b: a.backups, app: app, database: database}).target(ctx)
		if err != nil {
			return 0, err
		}
		out, err := runWrite(ctx, statement, t.container,
			"psql", "-X", "-v", "ON_ERROR_STOP=1", "--single-transaction", "-h", pgSocket, "-U", t.user, "-d", t.database)
		return pgAffected(string(out)), err
	case database.MySQL():
		t, err := (&mysqlEngine{b: a.backups, app: app, database: database}).target(ctx)
		if err != nil {
			return 0, err
		}
		// The client stops at the first error and never reaches COMMIT.
		// MySQL commits a CREATE, ALTER or DROP by itself.
		script := "START TRANSACTION;\n" + strings.TrimRight(statement, "; \t\r\n") + ";\nSELECT ROW_COUNT();\nCOMMIT;\n"
		out, err := runWrite(ctx, script, t.container,
			"sh", "-c", mysqlTool, "sh", mysqlClient[0], mysqlClient[1], "-uroot", "-N", "-B", t.database)
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		affected, _ := strconv.ParseInt(lines[len(lines)-1], 10, 64)
		return max(affected, 0), err
	}
	return sqliteWrite(ctx, database, statement)
}

// sqliteRead asks a SQLite file, opened read-only.
func sqliteRead(ctx context.Context, database protocol.Database, query string, limit int) (SQLResult, error) {
	if err := plainDatabase(database); err != nil {
		return SQLResult{}, err
	}
	db, err := sql.Open("sqlite", "file:"+database.HostPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return SQLResult{}, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return SQLResult{}, err
	}
	defer rows.Close()
	result := SQLResult{Rows: [][]*string{}}
	if result.Columns, err = rows.Columns(); err != nil {
		return SQLResult{}, err
	}
	for rows.Next() {
		if len(result.Rows) == limit {
			result.More = true
			break
		}
		values := make([]sql.NullString, len(result.Columns))
		targets := make([]any, len(values))
		for index := range values {
			targets[index] = &values[index]
		}
		if err := rows.Scan(targets...); err != nil {
			return SQLResult{}, err
		}
		row := make([]*string, len(values))
		for index, value := range values {
			if value.Valid {
				row[index] = &value.String
			}
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}

// sqliteWrite runs a statement on a SQLite file in one transaction. It
// waits up to 5 seconds for the app to finish its own write.
func sqliteWrite(ctx context.Context, database protocol.Database, statement string) (int64, error) {
	if err := plainDatabase(database); err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", "file:"+database.HostPath+"?mode=rw&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	changed, err := tx.ExecContext(ctx, statement)
	if err != nil {
		return 0, errors.Join(err, tx.Rollback())
	}
	affected, _ := changed.RowsAffected()
	return affected, tx.Commit()
}

// sqlCapped runs a command in a container and passes on only the first
// bytes of what it prints, $1 of them: head ends the command there, in
// the container. It exits as the command did.
const sqlCapped = `limit=$1
shift
exec 4>&1
status=$({ { "$@"; echo $? >&3; } | head -c "$limit" >&4; } 3>&1)
exit "$status"`

// runSQL feeds script to a database's client in its container and
// answers what the client printed. cut says the client had more to print
// than sqlMaxBytes. A failure says what the client said: it is your own
// query, on your own terminal.
func runSQL(ctx context.Context, script, container string, env []string, client ...string) (out []byte, cut bool, err error) {
	args := []string{"exec", "-i"}
	for _, each := range env {
		args = append(args, "-e", each)
	}
	args = append(args, container, "sh", "-c", sqlCapped, "sh", strconv.Itoa(sqlMaxBytes))
	command := exec.CommandContext(ctx, "docker", append(args, client...)...)
	var stdout, stderr bytes.Buffer
	command.Stdin, command.Stdout, command.Stderr = strings.NewReader(script), &stdout, &stderr
	err = command.Run()
	if stdout.Len() >= sqlMaxBytes {
		// head closed the pipe, which ended the client: no failure.
		return stdout.Bytes(), true, nil
	}
	if err != nil {
		if said := strings.TrimSpace(stderr.String()); said != "" {
			err = errors.New(said)
		}
		return nil, false, err
	}
	return stdout.Bytes(), false, nil
}

// sqlWriteTail is how much of what a write prints is kept: its end,
// where the client says how many rows changed.
const sqlWriteTail = 1 << 20

// lastBytes keeps the last max bytes written to it.
type lastBytes struct {
	kept []byte
	max  int
}

func (l *lastBytes) Write(p []byte) (int, error) {
	l.kept = append(l.kept, p...)
	if len(l.kept) > l.max {
		l.kept = l.kept[len(l.kept)-l.max:]
	}
	return len(p), nil
}

// runWrite feeds a script that may change data to a database's client in
// its container and answers the end of what the client printed. Nothing
// ends the client early, as runSQL does for a read: a statement that
// prints a lot, like one with RETURNING, still runs to its COMMIT.
func runWrite(ctx context.Context, script, container string, client ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "docker", append([]string{"exec", "-i", container}, client...)...)
	stdout := &lastBytes{max: sqlWriteTail}
	var stderr bytes.Buffer
	command.Stdin, command.Stdout, command.Stderr = strings.NewReader(script), stdout, &stderr
	if err := command.Run(); err != nil {
		if said := strings.TrimSpace(stderr.String()); said != "" {
			err = errors.New(said)
		}
		return nil, err
	}
	return stdout.kept, nil
}

// How psql prints the rows: the values of a row apart by pgField, the
// rows by pgRow, the control characters made for it, and pgNull for NULL,
// so it is not taken for an empty text. No value of a database looks like
// them, and a comma, a quote or a line break in one needs no escape.
const (
	pgField = "\x1f"
	pgRow   = "\x1e"
	pgNull  = "\x01null\x01"
)

// readPsql reads what psql printed: the columns, then the rows. cut says
// the end is missing, and with it the last row.
func readPsql(out []byte, limit int, cut bool) SQLResult {
	result := SQLResult{Columns: []string{}, Rows: [][]*string{}, More: cut}
	// psql ends what it prints with a line break.
	records := strings.Split(strings.TrimSuffix(string(out), "\n"), pgRow)
	if cut {
		records = records[:len(records)-1]
	}
	if len(records) == 0 || len(out) == 0 {
		return result
	}
	result.Columns = strings.Split(records[0], pgField)
	for _, record := range records[1:] {
		if len(result.Rows) == limit {
			result.More = true
			break
		}
		values := strings.Split(record, pgField)
		row := make([]*string, len(values))
		for index, value := range values {
			if value != pgNull {
				row[index] = &value
			}
		}
		result.Rows = append(result.Rows, row)
	}
	return result
}

// readXML reads what mysql --xml printed: a row of fields for every row,
// each with the name of its column, and xsi:nil for NULL. cut says the
// end is missing.
func readXML(out []byte, limit int, cut bool) (SQLResult, error) {
	result := SQLResult{Columns: []string{}, Rows: [][]*string{}, More: cut}
	decoder := xml.NewDecoder(bytes.NewReader(out))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) || err != nil && cut {
			return result, nil
		}
		if err != nil {
			return SQLResult{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}
		var row struct {
			Fields []struct {
				Name  string `xml:"name,attr"`
				Nil   bool   `xml:"nil,attr"`
				Value string `xml:",chardata"`
			} `xml:"field"`
		}
		if err := decoder.DecodeElement(&row, &start); err != nil {
			if cut {
				return result, nil
			}
			return SQLResult{}, err
		}
		if len(result.Rows) == limit {
			result.More = true
			return result, nil
		}
		values := make([]*string, len(row.Fields))
		names := make([]string, len(row.Fields))
		for index, field := range row.Fields {
			names[index] = field.Name
			if !field.Nil {
				values[index] = &field.Value
			}
		}
		if len(result.Rows) == 0 {
			result.Columns = names
		}
		result.Rows = append(result.Rows, values)
	}
}

// pgTag is what psql prints after a statement that changed rows, like
// UPDATE 3 or INSERT 0 1.
var pgTag = regexp.MustCompile(`^(?:INSERT \d+|UPDATE|DELETE|MERGE) (\d+)$`)

// pgAffected adds up the rows the statements of a psql run changed.
func pgAffected(out string) int64 {
	var affected int64
	for _, line := range strings.Split(out, "\n") {
		if match := pgTag.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			count, _ := strconv.ParseInt(match[1], 10, 64)
			affected += count
		}
	}
	return affected
}

// sqlNote is the note of the copy taken before the last `moin sql
// --write` of an app: the database and the copy.
type sqlNote struct {
	Database string `json:"database"`
	Copy     string `json:"copy"`
}

func (a *apps) beforeSQLFile(app string) string {
	return filepath.Join(a.appDir(app), BeforeSQL+".json")
}

// copyBeforeSQL copies a database into the bucket before a statement
// changes it, and notes the copy as before-sql.
func (b *backups) copyBeforeSQL(ctx context.Context, app string, database protocol.Database, out io.Writer) (string, error) {
	if !b.on() {
		return "", fmt.Errorf("a write takes a copy first, and this server has no bucket for it. Set one with `%s bucket set <server>`, or write without a copy: --no-copy", build.Name)
	}
	fmt.Fprintf(out, "copying %s\n", database.Path)
	id, err := b.copyNow(ctx, app, database)
	if err != nil {
		return "", fmt.Errorf("the copy before the write failed, so nothing ran: %w", err)
	}
	return id, writeState(b.apps.beforeSQLFile(app), sqlNote{Database: database.Path, Copy: id})
}

// beforeSQLCopy is the database and the copy that before-sql names.
func (a *apps) beforeSQLCopy(app string) (database, copy string, err error) {
	var note sqlNote
	body, _ := os.ReadFile(a.beforeSQLFile(app))
	if json.Unmarshal(body, &note) != nil || !protocol.ValidCopyID(note.Copy) {
		return "", "", fmt.Errorf("%s has no %s: no `%s sql --write` took a copy of it yet", app, BeforeSQL, build.Name)
	}
	return note.Database, note.Copy, nil
}
