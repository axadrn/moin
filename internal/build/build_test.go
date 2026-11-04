package build

import "testing"

// An old agent checks that the new binary says "ops <version>" before it
// swaps it in; called by any other name it says its own.
func TestInvoked(t *testing.T) {
	for arg0, want := range map[string]string{
		"/usr/local/bin/.ops-update-123456": LegacyName,
		"/usr/local/bin/ops":                LegacyName,
		"/usr/local/bin/moin":               Name,
		"moin":                              Name,
		"/tmp/.moin-update-1":               Name,
	} {
		if got := Invoked(arg0); got != want {
			t.Errorf("Invoked(%q) = %q, want %q", arg0, got, want)
		}
	}
}

// MOIN_* wins; OPS_* still counts while the old name works.
func TestGetenv(t *testing.T) {
	t.Setenv("OPS_URL", "old")
	if got := Getenv("URL"); got != "old" && Legacy() {
		t.Errorf("Getenv = %q, want the old variable", got)
	}
	t.Setenv("MOIN_URL", "new")
	if got := Getenv("URL"); got != "new" {
		t.Errorf("Getenv = %q, want new", got)
	}
}
