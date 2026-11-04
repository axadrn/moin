package cli

import (
	"reflect"
	"testing"
)

// TestExecArgs: everything after the first -- is the command's own, its
// flags and a second -- too; nothing is joined or split again.
func TestExecArgs(t *testing.T) {
	own, command := execArgs([]string{"shop", "--service", "db", "--", "psql", "-c", "select 1; drop table x", "--", "-v"})
	if !reflect.DeepEqual(own, []string{"shop", "--service", "db"}) {
		t.Errorf("own = %q", own)
	}
	if !reflect.DeepEqual(command, []string{"psql", "-c", "select 1; drop table x", "--", "-v"}) {
		t.Errorf("command = %q", command)
	}
	if _, command := execArgs([]string{"shop", "ls"}); command != nil {
		t.Errorf("without -- the command is %q", command)
	}
}
