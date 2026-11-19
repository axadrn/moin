package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRefuseIncludes refuses include, and extends of another file, in any
// document of a compose file.
func TestRefuseIncludes(t *testing.T) {
	for _, test := range []struct {
		name, compose, want string
	}{
		{"plain", "services:\n  web:\n    image: nginx\n", ""},
		{"extends in the file", "services:\n  base:\n    image: nginx\n  web:\n    extends: base\n", ""},
		{"extends a service of the file", "services:\n  base:\n    image: nginx\n  web:\n    extends: {service: base}\n", ""},
		{"include", "include: [db.yml]\nservices: {}\n", "include is not allowed"},
		{"include long", "include:\n  - path: /opt/other/compose.yml\n", "include is not allowed"},
		{"extends another file", "services:\n  web:\n    extends:\n      file: /var/lib/moin/apps/shop/releases/1/compose.json\n      service: web\n", "extends another file"},
		{"second document", "services:\n  web:\n    image: nginx\n---\ninclude: [/etc/x.yml]\n", "include is not allowed"},
		{"through an anchor", "x-base: &base\n  file: /etc/x.yml\n  service: web\nservices:\n  web:\n    extends: *base\n", "extends another file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "compose.yml")
			if err := os.WriteFile(file, []byte(test.compose), 0o644); err != nil {
				t.Fatal(err)
			}
			err := refuseIncludes(file)
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}
