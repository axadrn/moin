package moin

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

// TestFormatted: every .go and .templ file is formatted. "cd hub && task fmt"
// fixes it.
func TestFormatted(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_templ.go") {
			return nil
		}
		var formatted []byte
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch filepath.Ext(path) {
		case ".go":
			formatted, err = format.Source(source)
		case ".templ":
			formatted, err = formatTempl(source)
		default:
			return nil
		}
		if err != nil {
			t.Errorf("%s: %v", path, err)
		} else if !bytes.Equal(source, formatted) {
			t.Errorf("%s is not formatted, run: cd hub && task fmt", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func formatTempl(source []byte) ([]byte, error) {
	file, err := parser.ParseString(string(source))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := file.Write(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
