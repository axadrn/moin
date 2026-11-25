package content

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// TestTheBinaryHoldsTheTexts: everything the hub reads is in the binary, the
// files that start with an underscore too.
func TestTheBinaryHoldsTheTexts(t *testing.T) {
	texts := FS(false)
	for _, name := range []string{"blog/your-server-needs-no-open-port.md", "changelog.md", "llms.txt", "docs/getting-started.md", "legal/terms.md"} {
		if _, err := fs.Stat(texts, name); err != nil {
			t.Error(err)
		}
	}
}

// TestSecurityShowsTheRulesFile: the docs show the rules file a new server
// gets, as the install script writes it.
func TestSecurityShowsTheRulesFile(t *testing.T) {
	page, err := fs.ReadFile(FS(false), "docs/guides/security.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "```\n"+protocol.DaemonRules(build.Name)+"```\n") {
		t.Errorf("guides/security.md does not show the rules file:\n%s", protocol.DaemonRules(build.Name))
	}
}
