package protocol

import "testing"

// TestInstantHost: <app>.<ip with dashes>.<provider> for a public IPv4 and
// a known provider, nothing else.
func TestInstantHost(t *testing.T) {
	for _, test := range []struct{ app, ip, provider, want string }{
		{"shop", "203.0.113.7", "sslip.io", "shop.203-0-113-7.sslip.io"},
		{"shop", "203.0.113.7", "nip.io", "shop.203-0-113-7.nip.io"},
		{"shop", "203.0.113.7", "off", ""},
		{"shop", "203.0.113.7", "", ""},
		{"shop", "2a01:4f8::1", "sslip.io", ""},
		{"shop", "", "sslip.io", ""},
		{"", "203.0.113.7", "sslip.io", ""},
	} {
		if got := InstantHost(test.app, test.ip, test.provider); got != test.want {
			t.Errorf("InstantHost(%q, %q, %q) = %q, want %q", test.app, test.ip, test.provider, got, test.want)
		}
	}
}
