package protocol

import (
	"strings"
	"testing"
)

func TestValidSnapshotID(t *testing.T) {
	for id, ok := range map[string]bool{
		"20261031T140200Z":               true,
		"20261031T140200Z-":              true,
		"20261031T140200Z-before-sql":    true,
		"20261031T140200Z-before-sql-2":  true,
		"20261331T140200Z-daily":         false,
		"20261031T140200Z-../../secrets": false,
		"20261031T140200Z/other":         false,
		"20261031T140200Z-Daily":         false,
		"20261031T140200Z-" + long(65):   false,
		"2026-10-31":                     false,
		"":                               false,
	} {
		if got := ValidSnapshotID(id); got != ok {
			t.Errorf("ValidSnapshotID(%q) = %t, want %t", id, got, ok)
		}
	}
}

func TestValidRefName(t *testing.T) {
	for name, ok := range map[string]bool{
		"main":                 true,
		"feature/login-v2":     true,
		"v1.2.3":               true,
		"0a1b2c3d":             true,
		"release_2026.11":      true,
		"":                     false,
		"@":                    false,
		"-main":                false,
		"--upload-pack=evil":   false,
		"/main":                false,
		"main/":                false,
		"main.":                false,
		"a..b":                 false,
		"a//b":                 false,
		"a@{1}":                false,
		"has space":            false,
		"a~1":                  false,
		"a^":                   false,
		"a:b":                  false,
		"a?":                   false,
		"a*":                   false,
		"a[b":                  false,
		"a\\b":                 false,
		"tab\tname":            false,
		"feature/.hidden":      false,
		"branch.lock":          false,
		"a/b.lock/c":           false,
		long(MaxRefLength):     true,
		long(MaxRefLength + 1): false,
	} {
		if got := ValidRefName(name); got != ok {
			t.Errorf("ValidRefName(%q) = %t, want %t", name, got, ok)
		}
	}
}

func long(n int) string { return strings.Repeat("a", n) }
