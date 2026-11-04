package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestMetricsConnection: metrics.db sits next to the database; one in memory
// keeps its metrics in memory.
func TestMetricsConnection(t *testing.T) {
	got := MetricsConnection("./data/ops.db?_pragma=foreign_keys(1)")
	if !strings.HasPrefix(got, filepath.Join("data", "metrics.db")+"?") {
		t.Errorf("file database: %q", got)
	}
	if got := MetricsConnection("file:/var/lib/ops/ops.db"); !strings.HasPrefix(got, "/var/lib/ops/metrics.db?") {
		t.Errorf("file: prefix: %q", got)
	}
	for _, memory := range []string{":memory:", "file::memory:?cache=shared", ""} {
		if got := MetricsConnection(memory); got != ":memory:" {
			t.Errorf("%q: %q", memory, got)
		}
	}
	metrics, err := OpenMetrics(MetricsConnection(filepath.Join(t.TempDir(), "ops.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer metrics.Close()
	if _, err := metrics.Exec(`INSERT INTO metrics_minutes (target, minute) VALUES ('a', 60)`); err != nil {
		t.Errorf("metrics tables: %v", err)
	}
}
