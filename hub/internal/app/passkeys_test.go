package app

import (
	"net/url"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
)

// Passkeys are on only where a browser makes them, over HTTPS or on
// localhost, and go-webauthn takes the host. Any other APP_URL starts the
// hub with passkeys off.
func TestPasskeysFollowAppURL(t *testing.T) {
	for appURL, on := range map[string]bool{
		"https://hub.example.com": true,
		"http://localhost:8080":   true,
		"http://dev-01:7331":      false,
		"http://hub.local":        false,
		"http://hub:8080":         false,
		"https://127.0.0.1":       false,
		"https://127.1":           false,
	} {
		if got := newPasskeys(&config.Config{AppURL: appURL}, nil, nil, nil).On(); got != on {
			t.Errorf("APP_URL %s: passkeys on = %v, want %v", appURL, got, on)
		}
	}
}

// A password manager names a hub's passkeys after its address, so two hubs
// never look alike: the host of APP_URL, its port only when it is not the
// scheme's own.
func TestPasskeysAreNamedAfterTheHub(t *testing.T) {
	for appURL, want := range map[string]string{
		"https://moin.run":              "moin.run",
		"https://hub.example.com/":      "hub.example.com",
		"https://hub.example.com:443":   "hub.example.com",
		"https://hub.example.com:8443/": "hub.example.com:8443",
		"http://localhost:8080":         "localhost:8080",
		"http://localhost:80":           "localhost",
	} {
		parsed, err := url.Parse(appURL)
		if err != nil {
			t.Fatal(err)
		}
		if got := relyingPartyName(parsed); got != want {
			t.Errorf("APP_URL %s: name = %q, want %q", appURL, got, want)
		}
	}
}
