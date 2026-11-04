package service

import (
	"errors"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/protocol"
)

func TestReadOnlyError(t *testing.T) {
	readOnly := &AgentError{Message: "attempt to write a readonly database (8)"}
	if err := readOnlyError(readOnly); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("readonly answer = %v, want ErrReadOnly", err)
	}
	other := &AgentError{Message: "no such table: orders"}
	if err := readOnlyError(other); err != other {
		t.Fatalf("other answer = %v, want it unchanged", err)
	}
	if err := readOnlyError(nil); err != nil {
		t.Fatalf("nil = %v", err)
	}
	postgres := &AgentError{Message: "ERROR:  cannot execute UPDATE in a read-only transaction"}
	if err := readOnlyError(postgres); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Postgres answer = %v, want ErrReadOnly", err)
	}
}

// TestRowWhere: a row is named by its rowid or by every column of its
// primary key; key values only ever travel as params, names quoted.
func TestRowWhere(t *testing.T) {
	sqlite := protocol.DBTable{Name: "users", Type: "table", RowID: true, Columns: []protocol.DBColumn{{Name: "id", PrimaryKey: true}}}
	where, params, err := rowWhere(sqlite, RowKey{RowID: 7})
	if err != nil || where != ` WHERE rowid = ?` || len(params) != 1 || params[0] != int64(7) {
		t.Errorf("rowid: %q %v %v", where, params, err)
	}
	postgres := protocol.DBTable{Name: "members", Type: "table", Columns: []protocol.DBColumn{{Name: "team", PrimaryKey: true}, {Name: `us"er`, PrimaryKey: true}, {Name: "role"}}}
	evil := "x'); DROP TABLE members; --"
	where, params, err = rowWhere(postgres, RowKey{Key: map[string]string{"team": evil, `us"er`: "1"}})
	if err != nil || where != ` WHERE "team" = ? AND "us""er" = ?` || params[0] != evil || params[1] != "1" {
		t.Errorf("key: %q %v %v", where, params, err)
	}
	for _, key := range []map[string]string{{"team": "a"}, {"team": "a", `us"er`: "1", "role": "admin"}, {}} {
		if _, _, err := rowWhere(postgres, RowKey{Key: key}); !errors.Is(err, ErrNoKey) {
			t.Errorf("key %v: %v", key, err)
		}
	}
	if Editable(protocol.DBTable{Type: "table", Columns: []protocol.DBColumn{{Name: "a"}}}) || !Editable(postgres) || Editable(protocol.DBTable{Type: "view", RowID: true}) {
		t.Error("Editable")
	}
}

// TestNewestFirst: a table without rowid pages by its primary key, newest
// first; a view or a table without one keeps the database's order.
func TestNewestFirst(t *testing.T) {
	table := protocol.DBTable{Name: "orders", Type: "table", Columns: []protocol.DBColumn{{Name: "shop", PrimaryKey: true}, {Name: `we"ird`, PrimaryKey: true}, {Name: "total"}}}
	if got := newestFirst(table); got != ` ORDER BY "shop" DESC, "we""ird" DESC` {
		t.Errorf("newestFirst = %q", got)
	}
	table.Type = "view"
	if got := newestFirst(table); got != "" {
		t.Errorf("view: %q", got)
	}
	if got := newestFirst(protocol.DBTable{Type: "table", Columns: []protocol.DBColumn{{Name: "a"}}}); got != "" {
		t.Errorf("no key: %q", got)
	}
}

func TestKeptDeploysFollowsTheAgentRule(t *testing.T) {
	// Newest first: v8 restart (live), v7 to v3 builds, v2 restart, v1 build,
	// and v9 a failed redeploy.
	deploys := []model.Deploy{{Number: 9, Status: model.DeployFailed, SourceKey: "server"}, {Number: 8, Status: model.DeploySucceeded}}
	for number := 7; number >= 3; number-- {
		deploys = append(deploys, model.Deploy{Number: number, Status: model.DeploySucceeded, SourceKey: "server"})
	}
	deploys = append(deploys, model.Deploy{Number: 2, Status: model.DeploySucceeded}, model.Deploy{Number: 1, Status: model.DeploySucceeded, Image: "ghcr.io/a/b:1"})
	kept := KeptDeploys(deploys, 8)
	for number, want := range map[int]bool{9: false, 8: true, 7: true, 3: true, 2: false, 1: false} {
		if kept[number] != want {
			t.Errorf("v%d kept = %v, want %v", number, kept[number], want)
		}
	}
}
