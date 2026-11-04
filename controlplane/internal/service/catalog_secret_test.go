package service

import (
	"encoding/hex"
	"testing"
)

// TestNewSecret: plain names get URL-safe text; _HEX names hex of 32
// bytes, _HEX16 names hex of 16, for apps that want a key in hex.
func TestNewSecret(t *testing.T) {
	if got := newSecret("APP_SECRET"); len(got) != 43 {
		t.Errorf("APP_SECRET = %d chars", len(got))
	}
	for name, size := range map[string]int{"SECRET_KEY_HEX": 64, "ENCRYPTION_KEY_HEX16": 32} {
		got := newSecret(name)
		if _, err := hex.DecodeString(got); err != nil || len(got) != size {
			t.Errorf("%s = %q (%d chars), want %d hex digits", name, got, len(got), size)
		}
	}
	if newSecret("A_HEX") == newSecret("A_HEX") {
		t.Error("two secrets are the same")
	}
}
