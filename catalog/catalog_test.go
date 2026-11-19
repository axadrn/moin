package catalog

import (
	"io/fs"
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
