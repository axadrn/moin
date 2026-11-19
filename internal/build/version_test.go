package build

import "testing"

func TestNewerVersion(t *testing.T) {
	for _, test := range []struct {
		candidate, current string
		newer              bool
	}{
		{"v0.3.0", "v0.2.9", true},
		{"v0.2.9", "v0.3.0", false},
		{"v0.3.0", "v0.3.0", false},
		{"v0.10.0", "v0.9.0", true},
		{"v1.0.0", "v1.0.0-beta.3", true},
		{"v1.0.0-beta.3", "v1.0.0", false},
		{"v1.0.0-beta.11", "v1.0.0-beta.3", true},
		{"v1.0.0-beta.3", "v1.0.0-alpha.9", true},
		{"v1.0.0-beta", "v1.0.0-beta.1", false},
		{"v1.0.0-rc.1", "v1.0.0-1", true},
		{"v1.0.1+build.5", "v1.0.0", true},
		{"v1.0.0", "dev", false},
		{"dev", "v1.0.0", false},
		{"v1.0", "v0.9.0", false},
		{"1.2.3", "v0.9.0", false},
		{"v01.2.3", "v0.9.0", false},
		{"v1.0.0", "main-abc1234", false},
	} {
		if got := NewerVersion(test.candidate, test.current); got != test.newer {
			t.Errorf("NewerVersion(%q, %q) = %v, want %v", test.candidate, test.current, got, test.newer)
		}
	}
}

func TestIsRelease(t *testing.T) {
	for version, want := range map[string]bool{
		"v1.0.0":         true,
		"v1.0.0-beta.33": true,
		"v1.0.0+build.7": true,
		"dev":            false,
		// A version goes into a download URL, so only semver characters.
		"v1.0.0-a/../b":  false,
		"v1.0.0-%2e%2e":  false,
		"v1.0.0+/../x":   false,
		"v1.0.0-beta..1": false,
		"main-b4be272":   false,
		"1.0.0":          false,
		"":               false,
	} {
		if got := IsRelease(version); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", version, got, want)
		}
	}
}
