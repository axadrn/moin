package secret

import (
	"bytes"
	"testing"
)

func TestSealOpen(t *testing.T) {
	box, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.Seal("hunter2", "app/DB_URL")
	if sealed == box.Seal("hunter2", "app/DB_URL") {
		t.Fatal("two seals of one value must differ")
	}
	value, err := box.Open(sealed, "app/DB_URL")
	if err != nil || value != "hunter2" {
		t.Fatalf("open = %q, %v", value, err)
	}
	if _, err := box.Open(sealed, "other/DB_URL"); err != ErrOpen {
		t.Fatalf("open with another context = %v, want ErrOpen", err)
	}
	other, _ := New(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(sealed, "app/DB_URL"); err != ErrOpen {
		t.Fatalf("open with another key = %v, want ErrOpen", err)
	}
}
