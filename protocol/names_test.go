package protocol

import "testing"

func TestValidCopyID(t *testing.T) {
	for id, ok := range map[string]bool{
		"20261031T140200Z":        true,
		"20261031T140200Z-":       false,
		"20261031T140200Z-manual": false,
		"20261331T140200Z":        false,
		"20261031T140200Z/../x":   false,
		"../20261031T140200Z":     false,
		"20261031T140200Z" + "\n": false,
		"2026-10-31":              false,
		"":                        false,
	} {
		if got := ValidCopyID(id); got != ok {
			t.Errorf("ValidCopyID(%q) = %t, want %t", id, got, ok)
		}
	}
}
