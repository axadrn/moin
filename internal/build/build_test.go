package build

import "testing"

// Only MOIN_* counts: the variables of the old name are ignored.
func TestGetenv(t *testing.T) {
	t.Setenv("MOIN_URL", "")
	t.Setenv("OPS_URL", "old")
	if got := Getenv("URL"); got != "" {
		t.Errorf("Getenv = %q, want the old variable ignored", got)
	}
	t.Setenv("MOIN_URL", "new")
	if got := Getenv("URL"); got != "new" {
		t.Errorf("Getenv = %q, want new", got)
	}
}
