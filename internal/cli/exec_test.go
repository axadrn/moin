package cli

import (
	"bytes"
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

// TestRawKeepsBytes: bytes with escapes, like a dump or a copy, pass raw
// as they are, where the writer for colors would take the escapes out.
func TestRawKeepsBytes(t *testing.T) {
	data := []byte("PGDMP\x1b[31m\x00\xff\x1b]8;;x\x07\x9b2J")
	var colored, plain bytes.Buffer
	terminal(&colored).Write(data)
	if bytes.Equal(colored.Bytes(), data) {
		t.Fatal("the writer for colors left the bytes as they are; the test shows nothing")
	}
	raw(terminal(&plain)).Write(data)
	if !bytes.Equal(plain.Bytes(), data) {
		t.Errorf("raw wrote %q, want %q", plain.Bytes(), data)
	}
}
