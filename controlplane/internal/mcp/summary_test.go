package mcp

import "testing"

func TestEnvSetSummary(t *testing.T) {
	cases := []struct {
		set, unset int
		want       string
	}{
		{1, 0, "Set 1 env var. Restart the app to apply it: restart_app."},
		{3, 0, "Set 3 env vars. Restart the app to apply them: restart_app."},
		{0, 1, "Removed 1 env var. Restart the app to apply it: restart_app."},
		{2, 1, "Set 2 env vars and removed 1 env var. Restart the app to apply them: restart_app."},
		{0, 0, "Nothing changed."},
	}
	for _, c := range cases {
		if got := (EnvSet{set: c.set, unset: c.unset}).Summary(); got != c.want {
			t.Errorf("set %d unset %d: %q, want %q", c.set, c.unset, got, c.want)
		}
	}
}
