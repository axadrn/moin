package service

import (
	"errors"
	"testing"
)

// A webhook takes any http or https URL without a password, and lists
// show the scheme and the host, never the path.
func TestWebhookDestination(t *testing.T) {
	for _, bad := range []string{"ftp://example.com", "https://user:pass@example.com", "example.com/hook", "https://"} {
		if _, err := (webhookDestination{}).check(AlertDestinationInput{URL: bad}); !errors.Is(err, ErrAlertURL) {
			t.Errorf("check(%q) = %v, want ErrAlertURL", bad, err)
		}
	}
	for address, hint := range map[string]string{
		"https://example.com":            "https://example.com",
		"https://example.com/":           "https://example.com",
		"http://10.0.0.5:8080/hook/abc1": "http://10.0.0.5:8080/…",
	} {
		to, err := webhookDestination{}.check(AlertDestinationInput{URL: address})
		if err != nil || to.target != address {
			t.Fatalf("check(%q) = %+v, %v", address, to, err)
		}
		if got := (webhookDestination{}).hint(to); got != hint {
			t.Errorf("hint(%q) = %q, want %q", address, got, hint)
		}
	}
}
