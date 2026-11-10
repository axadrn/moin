package content

import (
	"io/fs"
	"testing"
)

// TestTheBinaryHoldsTheTexts: everything the hub reads is in the binary, the
// files that start with an underscore too.
func TestTheBinaryHoldsTheTexts(t *testing.T) {
	texts := FS(false)
	for _, name := range []string{"changelog.md", "llms.txt", "docs/getting-started.md", "legal/terms.md", "catalog"} {
		if _, err := fs.Stat(texts, name); err != nil {
			t.Error(err)
		}
	}
}
