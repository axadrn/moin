package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

func TestRulesReadEveryRule(t *testing.T) {
	rules, err := parseRules(`
# what moin may do here
data: off
logs: off   # no logs through moin
network: locked
deploy-from: github.com/acme/*, ghcr.io/acme/*, catalog
backups-to: s3.eu-central-1.amazonaws.com/acme-backups
`, "rules")
	if err != nil {
		t.Fatal(err)
	}
	want := protocol.Rules{DataOff: true, LogsOff: true, NetworkLocked: true, DeployFrom: []string{"github.com/acme/*", "ghcr.io/acme/*", "catalog"}, BackupsTo: "s3.eu-central-1.amazonaws.com/acme-backups"}
	if strings.Join(rules.DeployFrom, ",") != strings.Join(want.DeployFrom, ",") || rules.DataOff != want.DataOff || rules.LogsOff != want.LogsOff || rules.NetworkLocked != want.NetworkLocked || rules.BackupsTo != want.BackupsTo {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestRulesWithAMistakeAreAnError(t *testing.T) {
	for _, text := range []string{"dta: off", "data off", "data: no", "network: closed", "backups-to: acme-backups", "deploy-from: github.com/[acme"} {
		if _, err := parseRules(text, "rules"); err == nil {
			t.Errorf("%q: no error", text)
		}
	}
}

func TestNoRulesFileAllowsEverything(t *testing.T) {
	rules, err := LoadRules(filepath.Join(t.TempDir(), "rules"))
	if err != nil || rules != nil {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	if checkDeploy(rules, "rules", protocol.DeployStart{Image: "evil.example/x:1"}) != nil {
		t.Fatal("a deploy without rules is refused")
	}
}

func TestDeployFrom(t *testing.T) {
	rules := &protocol.Rules{DeployFrom: []string{"github.com/acme/*", "ghcr.io/acme/*"}}
	cases := []struct {
		start protocol.DeployStart
		ok    bool
	}{
		{protocol.DeployStart{SourceURL: "https://api.github.com/repos/acme/blog/tarball/abc123"}, true},
		{protocol.DeployStart{SourceURL: "https://api.github.com/repos/evil/blog/tarball/abc123"}, false},
		{protocol.DeployStart{SourceURL: "https://evil.example/repos/acme/blog/tarball/abc123"}, false},
		{protocol.DeployStart{SourceURL: "https://bucket.example/uploads/x.tar.gz"}, false},
		{protocol.DeployStart{Image: "ghcr.io/acme/blog:v3"}, true},
		{protocol.DeployStart{Image: "ghcr.io/acme/blog@sha256:abc"}, true},
		{protocol.DeployStart{Image: "ghcr.io/evil/blog:v3"}, false},
		{protocol.DeployStart{SourceNumber: 4}, true},
		{protocol.DeployStart{FromNumber: 4}, true},
		// An image with the compose file of a commit brings both sources.
		{protocol.DeployStart{SourceURL: "https://api.github.com/repos/acme/blog/tarball/abc123", Image: "ghcr.io/acme/blog:v3"}, true},
		{protocol.DeployStart{SourceURL: "https://api.github.com/repos/acme/blog/tarball/abc123", Image: "ghcr.io/evil/blog:v3"}, false},
		{protocol.DeployStart{SourceURL: "https://api.github.com/repos/evil/blog/tarball/abc123", Image: "ghcr.io/acme/blog:v3"}, false},
	}
	for _, c := range cases {
		err := checkDeploy(rules, "rules", c.start)
		if (err == nil) != c.ok {
			t.Errorf("%+v: err = %v", c.start, err)
		}
	}
}

// A source is the GitHub tarball of a commit, with rules or without:
// the daemon downloads nothing else.
func TestSourcesComeOnlyFromGitHub(t *testing.T) {
	if name, err := sourceName("https://api.github.com/repos/acme/blog/tarball/abc123"); err != nil || name != "github.com/acme/blog" {
		t.Fatalf("the GitHub tarball = %q, %v", name, err)
	}
	for _, raw := range []string{"https://bucket.example/acme/uploads/x.tar.gz", "https://api.github.com/repos/acme/blog/zipball/abc123"} {
		err := download(context.Background(), protocol.DeployStart{SourceURL: raw}, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "only from a GitHub repo") {
			t.Errorf("%s: err = %v", raw, err)
		}
		if checkDeploy(&protocol.Rules{DeployFrom: []string{"*"}}, "rules", protocol.DeployStart{SourceURL: raw}) == nil {
			t.Errorf("%s: the rules allow it", raw)
		}
	}
}

func TestBackupsTo(t *testing.T) {
	rules := &protocol.Rules{BackupsTo: "s3.eu-central-1.amazonaws.com/acme"}
	if checkBucket(rules, "rules", &protocol.Bucket{Region: "eu-central-1", Bucket: "acme"}) != nil {
		t.Fatal("the allowed AWS bucket is refused")
	}
	if checkBucket(rules, "rules", &protocol.Bucket{Endpoint: "https://evil.example", Bucket: "acme"}) == nil {
		t.Fatal("another bucket is allowed")
	}
	if checkBucket(rules, "rules", nil) != nil {
		t.Fatal("turning backups off is refused")
	}
}

func TestHelloCarriesTheRules(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rules")
	os.WriteFile(file, []byte("data: off\n"), 0o600)
	t.Setenv("MOIN_RULES", file)
	if hello := Hello(t.TempDir()); hello.Rules == nil || !hello.Rules.DataOff {
		t.Fatalf("hello = %+v", hello)
	}
}

// OPS_RULES of the old name counts for nothing: only MOIN_RULES, else
// /etc/moin/rules.
func TestRulesFileOldNameIgnored(t *testing.T) {
	t.Setenv("MOIN_RULES", "")
	t.Setenv("OPS_RULES", filepath.Join(t.TempDir(), "rules"))
	if got := RulesFile(); got != "/etc/moin/rules" {
		t.Fatalf("RulesFile = %q", got)
	}
}
