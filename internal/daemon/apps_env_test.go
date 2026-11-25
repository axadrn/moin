package daemon

import (
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestGenerateEnv: the server makes a value for each key of Generate the
// app has none for, and keeps the ones it has, so the hub never sees them.
func TestGenerateEnv(t *testing.T) {
	a := newApps(t.TempDir(), nil)
	if _, err := a.setEnv(protocol.EnvSet{App: "web", Set: map[string]string{"DB_PASSWORD": "kept"}}); err != nil {
		t.Fatal(err)
	}
	kept, err := a.setEnv(protocol.EnvSet{App: "web", Generate: []string{"DB_PASSWORD", "APP_SECRET"}})
	if err != nil {
		t.Fatal(err)
	}
	env, err := a.env("web")
	if err != nil {
		t.Fatal(err)
	}
	if env["DB_PASSWORD"] != "kept" || len(env["APP_SECRET"]) != 43 || len(kept.Keys) != 2 {
		t.Errorf("env = %v, keys %v", env, kept.Keys)
	}
	if _, err := a.setEnv(protocol.EnvSet{App: "web", Generate: []string{"bad key"}}); err == nil {
		t.Error("a bad key was made")
	}
}
