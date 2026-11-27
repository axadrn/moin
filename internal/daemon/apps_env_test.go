package daemon

import (
	"testing"

	"github.com/axadrn/moin/catalog"
	"github.com/axadrn/moin/protocol"
)

// TestMakeSecrets: a catalog deploy makes a value for each secret its
// compose file needs that the app lacks, and keeps the ones it has.
func TestMakeSecrets(t *testing.T) {
	a := newApps(t.TempDir())
	if _, err := a.setEnv(protocol.EnvSet{App: "flux", Set: map[string]string{"VERSION": "2.2.0", "ADMIN_PASSWORD": "kept"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.makeSecrets("flux", "miniflux"); err != nil {
		t.Fatal(err)
	}
	env, err := a.env("flux")
	if err != nil {
		t.Fatal(err)
	}
	compose, _ := catalog.Compose("miniflux")
	for _, name := range catalog.Secrets(compose) {
		if env[name] == "" {
			t.Errorf("%s was not made", name)
		}
	}
	if env["ADMIN_PASSWORD"] != "kept" || env["VERSION"] != "2.2.0" {
		t.Errorf("env = %v", env)
	}
	made := env["DB_PASSWORD"]
	if err := a.makeSecrets("flux", "miniflux"); err != nil {
		t.Fatal(err)
	}
	if again, _ := a.env("flux"); again["DB_PASSWORD"] != made {
		t.Error("a second deploy made the secret again")
	}
	if err := a.makeSecrets("flux", "no-such-app"); err == nil {
		t.Error("an app the catalog lacks made secrets")
	}
}
