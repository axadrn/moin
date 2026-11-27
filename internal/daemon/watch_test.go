package daemon

import (
	"bytes"
	"log/slog"
	"testing"
)

// journal catches what the daemon writes to its log during the test.
func journal(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &out
}
