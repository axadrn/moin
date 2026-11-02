package client

import "testing"

func TestCheckURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://ops.example.com":      true,
		"https://1.2.3.4:8443":         true,
		"http://localhost:7331":        true,
		"http://127.0.0.1:7331":        true,
		"http://[::1]:7331":            true,
		"http://100.101.102.103:7331":  true,
		"http://ops.tail1234.ts.net":   true,
		"http://ops.example.com":       false,
		"http://1.2.3.4":               false,
		"http://100.128.0.1":           false,
		"http://ts.net.example.com":    false,
		"http://localhost.example.com": false,
		"ftp://ops.example.com":        false,
		"ops.example.com":              false,
		"":                             false,
	} {
		if err := CheckURL(raw); (err == nil) != ok {
			t.Errorf("CheckURL(%q) = %v, want ok %t", raw, err, ok)
		}
	}
}
