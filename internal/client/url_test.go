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

func TestCheckURLShortTailnetName(t *testing.T) {
	answers := map[string][]string{
		"dev-01":  {"100.101.102.103", "fd7a:115c:a1e0::1"},
		"printer": {"192.168.1.20"},
		"mixed":   {"100.101.102.103", "203.0.113.9"},
	}
	defer func(original func(string) ([]string, error)) { lookupHost = original }(lookupHost)
	lookupHost = func(host string) ([]string, error) { return answers[host], nil }

	if err := CheckURL("http://dev-01:7331"); err != nil {
		t.Fatalf("a MagicDNS name of the tailnet: %v", err)
	}
	for _, host := range []string{"printer", "mixed", "unknown", "example.com"} {
		if err := CheckURL("http://" + host + ":7331"); err == nil {
			t.Fatalf("http://%s was accepted", host)
		}
	}
}
