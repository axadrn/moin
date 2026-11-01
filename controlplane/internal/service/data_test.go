package service

import (
	"errors"
	"testing"
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
}
