package client

import "testing"

func TestCheckURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://moin.example.com":     true,
		"https://1.2.3.4:8443":         true,
		"http://localhost:7331":        true,
		"http://127.0.0.1:7331":        true,
		"http://[::1]:7331":            true,
		"http://100.101.102.103:7331":  true,
		"http://moin.tail1234.ts.net":  true,
		"http://moin.example.com":      false,
		"http://1.2.3.4":               false,
		"http://100.128.0.1":           false,
		"http://ts.net.example.com":    false,
		"http://localhost.example.com": false,
		"ftp://moin.example.com":       false,
		"moin.example.com":             false,
		"":                             false,
	} {
		if err := CheckURL(raw); (err == nil) != ok {
			t.Errorf("CheckURL(%q) = %v, want ok %t", raw, err, ok)
		}
	}
}

// The CLI capitalizes errors, so the refusal starts with a word, not the URL.
func TestCheckURLRefusalReadsWell(t *testing.T) {
	err := CheckURL("http://example.com")
	if err == nil || err.Error() != "plain http sends your token unencrypted to example.com. Use https://example.com, or http only on localhost or your tailnet" {
		t.Errorf("err = %v", err)
	}
}
