package catalog

import (
	"encoding/hex"
	"io/fs"
	"slices"
	"testing"
)

// TestTheBinaryHoldsTheCatalog: every app's files are built in, so the
// daemon finds each compose file the hub names.
func TestTheBinaryHoldsTheCatalog(t *testing.T) {
	folders, err := fs.ReadDir(Files, ".")
	if err != nil || len(folders) < 8 {
		t.Fatalf("%d folders, %v", len(folders), err)
	}
	for _, folder := range folders {
		if _, ok := Compose(folder.Name()); !ok {
			t.Errorf("%s has no compose.yml", folder.Name())
		}
		if _, err := fs.Stat(Files, folder.Name()+"/app.md"); err != nil {
			t.Error(err)
		}
	}
}

func TestSecrets(t *testing.T) {
	compose := []byte("image: a:${VERSION}\nA: ${DB_PASSWORD}\nB: ${APP_SECRET}\nC: ${DB_PASSWORD}\nD: ${OPTIONAL:-x}\nE: $PLAIN\nF: ${APP_URL}\n")
	if got := Secrets(compose); !slices.Equal(got, []string{"APP_SECRET", "DB_PASSWORD"}) {
		t.Errorf("Secrets = %v", got)
	}
}

// TestNewSecret: plain names get URL-safe text; _HEX names hex of 32
// bytes, _HEX16 names hex of 16, for apps that want a key in hex.
func TestNewSecret(t *testing.T) {
	if got := NewSecret("APP_SECRET"); len(got) != 43 {
		t.Errorf("APP_SECRET = %d chars", len(got))
	}
	for name, size := range map[string]int{"SECRET_KEY_HEX": 64, "ENCRYPTION_KEY_HEX16": 32} {
		got := NewSecret(name)
		if _, err := hex.DecodeString(got); err != nil || len(got) != size {
			t.Errorf("%s = %q (%d chars), want %d hex digits", name, got, len(got), size)
		}
	}
	if NewSecret("A_HEX") == NewSecret("A_HEX") {
		t.Error("two secrets are the same")
	}
}
