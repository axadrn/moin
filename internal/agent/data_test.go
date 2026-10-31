package agent

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/ops/protocol"
)

func TestTablesAndQueries(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir, func(protocol.MessageType, string, any) {})
	file := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL;
		CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT NOT NULL, body BLOB);
		CREATE TABLE tags (name TEXT PRIMARY KEY) WITHOUT ROWID;
		CREATE VIEW titles AS SELECT title FROM posts;
		CREATE INDEX posts_title ON posts (title);
		CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY AUTOINCREMENT, version_id INTEGER NOT NULL, is_applied INTEGER NOT NULL, tstamp TIMESTAMP DEFAULT (datetime('now')));
		INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (0, 1, '2026-10-01 10:00:00'), (20261030, 1, '2026-10-30 12:00:00');
		INSERT INTO posts (title, body) VALUES ('hello', x'00ff'), ('world', 'text');`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	release := a.releaseDir("blog", 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{{Path: "/data/app.db", HostPath: file}}})
	os.WriteFile(filepath.Join(a.appDir("blog"), "current"), []byte("1"), 0o600)
	ctx := context.Background()

	tables, err := a.tables(ctx, protocol.DBRequest{App: "blog"})
	if err != nil {
		t.Fatal(err)
	}
	if tables.Migration == nil || tables.Migration.Tool != "goose" || tables.Migration.Version != "20261030" || tables.Migration.AppliedAt == "" {
		t.Fatalf("migration = %+v", tables.Migration)
	}
	tables.Tables = slices.DeleteFunc(tables.Tables, func(table protocol.DBTable) bool { return strings.HasPrefix(table.Name, "goose") })
	if tables.Database != "/data/app.db" || len(tables.Tables) != 3 {
		t.Fatalf("tables = %+v", tables)
	}
	posts := tables.Tables[0]
	if posts.Name != "posts" || posts.Rows != 2 || !posts.RowID || len(posts.Columns) != 3 || !posts.Columns[0].PrimaryKey || !posts.Columns[1].NotNull {
		t.Fatalf("posts = %+v", posts)
	}
	if !strings.HasPrefix(posts.SQL, "CREATE TABLE posts") || len(posts.Indexes) != 1 {
		t.Fatalf("posts schema = %q, %v", posts.SQL, posts.Indexes)
	}
	if tags := tables.Tables[1]; tags.Name != "tags" || tags.RowID {
		t.Fatalf("a WITHOUT ROWID table is editable by rowid: %+v", tags)
	}
	if view := tables.Tables[2]; view.Type != "view" || view.RowID {
		t.Fatalf("view = %+v", view)
	}

	result, err := a.query(ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: "blog"}, SQL: `SELECT rowid, title, body FROM posts ORDER BY id`, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || !result.Truncated || result.Rows[0][1] != "hello" || result.Rows[0][2] != "0x00ff" {
		t.Fatalf("read = %+v", result)
	}

	_, err = a.query(ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: "blog"}, SQL: `DELETE FROM posts`})
	if err == nil {
		t.Fatal("a read deleted rows")
	}
	result, err = a.query(ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: "blog", Database: "/data/app.db"}, SQL: `UPDATE posts SET title = ? WHERE rowid = ?`, Params: []any{"hi", 1}, Write: true})
	if err != nil || result.Changes != 1 {
		t.Fatalf("write = %+v, %v", result, err)
	}
	result, _ = a.query(ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: "blog"}, SQL: `SELECT title FROM posts WHERE id = 1`})
	if result.Rows[0][0] != "hi" {
		t.Fatalf("after write = %+v", result)
	}
	if _, err := a.query(ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: "blog", Database: "/other.db"}, SQL: `SELECT 1`}); err == nil {
		t.Fatal("an unknown database answered")
	}
}
