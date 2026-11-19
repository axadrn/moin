package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckSourceFiles: include, extends and env_file name files of the
// source only, never of the server, and never through a variable.
func TestCheckSourceFiles(t *testing.T) {
	for _, test := range []struct {
		name, compose, want string
	}{
		{"plain", "services:\n  web:\n    image: nginx\n", ""},
		{"env file of the source", "services:\n  web:\n    image: nginx\n    env_file: [app.env, {path: more.env, required: false}]\n", ""},
		{"include of the source", "include: [db.yml]\nservices:\n  web:\n    image: nginx\n", ""},
		{"extends in the file", "services:\n  base:\n    image: nginx\n  web:\n    extends: base\n", ""},
		{"env file of the server", "services:\n  web:\n    image: nginx\n    env_file: /root/.aws/credentials\n", "/root/.aws/credentials is not in the app's source"},
		{"env file above", "services:\n  web:\n    image: nginx\n    env_file: [{path: ../../../x.env}]\n", "is not in the app's source"},
		{"include of the server", "include:\n  - path: /opt/other/compose.yml\nservices: {}\n", "/opt/other/compose.yml is not in the app's source"},
		{"include env file", "include:\n  - path: db.yml\n    env_file: /etc/moin/daemon.env\nservices: {}\n", "/etc/moin/daemon.env is not in the app's source"},
		{"extends of the server", "services:\n  web:\n    extends:\n      file: /var/lib/moin/apps/shop/releases/1/compose.json\n      service: web\n", "is not in the app's source"},
		{"variable", "services:\n  web:\n    image: nginx\n    env_file: ${HOME}/.env\n", "has a variable"},
		{"included file includes the server", "include: [db.yml]\nservices: {}\n", "/etc/shadow is not in the app's source"},
	} {
		t.Run(test.name, func(t *testing.T) {
			src := t.TempDir()
			db := "services:\n  db:\n    image: postgres\n"
			if test.name == "included file includes the server" {
				db = "services:\n  db:\n    image: postgres\n    env_file: /etc/shadow\n"
			}
			files := map[string]string{"compose.yml": test.compose, "db.yml": db}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := checkSourceFiles(src)
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}
