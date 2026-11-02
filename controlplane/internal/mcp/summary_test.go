package mcp

import "testing"

func TestEnvSetSummary(t *testing.T) {
	cases := []struct {
		set, removed int
		missing      []string
		want         string
	}{
		{1, 0, nil, "Set 1 env var. Restart the app to apply it: restart_app."},
		{3, 0, nil, "Set 3 env vars. Restart the app to apply them: restart_app."},
		{0, 1, nil, "Removed 1 env var. Restart the app to apply it: restart_app."},
		{2, 1, nil, "Set 2 env vars and removed 1 env var. Restart the app to apply them: restart_app."},
		{0, 0, nil, "Nothing changed."},
		{0, 0, []string{"NOT_THERE"}, "Nothing changed. Not set, so not removed: NOT_THERE."},
		{0, 1, []string{"NOT_THERE"}, "Removed 1 env var. Not set, so not removed: NOT_THERE. Restart the app to apply it: restart_app."},
	}
	for _, c := range cases {
		if got := (EnvSet{set: c.set, removed: c.removed, missing: c.missing}).Summary(); got != c.want {
			t.Errorf("set %d removed %d missing %v: %q, want %q", c.set, c.removed, c.missing, got, c.want)
		}
	}
}
