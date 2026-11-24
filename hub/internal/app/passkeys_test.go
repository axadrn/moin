package app

import (
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
