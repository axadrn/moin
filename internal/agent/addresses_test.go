package agent

import (
	"net/netip"
	"testing"
)

func TestIsPublic(t *testing.T) {
	cases := map[string]bool{
		"203.0.113.7":   true,
		"2a01:4f8::1":   true,
		"10.0.0.4":      false,
		"192.168.1.2":   false,
		"100.101.102.3": false,
		"127.0.0.1":     false,
		"fd00::1":       false,
		"fe80::1":       false,
	}
	for text, want := range cases {
		if got := isPublic(netip.MustParseAddr(text)); got != want {
			t.Errorf("isPublic(%s) = %v, want %v", text, got, want)
		}
	}
}
