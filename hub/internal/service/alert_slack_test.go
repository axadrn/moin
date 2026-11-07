package service

import (
	"errors"
	"testing"
)

// Slack takes only its own incoming webhooks, over https, and lists show
// the host and the end of the URL.
func TestSlackDestination(t *testing.T) {
	for _, bad := range []string{"https://example.com/services/a", "http://hooks.slack.com/services/a", "https://hooks.slack.com/other", "https://u:p@hooks.slack.com/services/a"} {
		if _, err := (slackDestination{}).check(AlertDestinationInput{URL: bad}); !errors.Is(err, ErrAlertURL) {
			t.Errorf("check(%q) = %v, want ErrAlertURL", bad, err)
		}
	}
	to, err := slackDestination{}.check(AlertDestinationInput{URL: " https://hooks.slack.com/services/T0/B0/abcdWXYZ ", ChatID: "ignored"})
	if err != nil || to != (alertTarget{target: "https://hooks.slack.com/services/T0/B0/abcdWXYZ"}) {
		t.Fatalf("check = %+v, %v", to, err)
	}
	if hint := (slackDestination{}).hint(to); hint != "hooks.slack.com/…WXYZ" {
		t.Errorf("hint = %q", hint)
	}
}
