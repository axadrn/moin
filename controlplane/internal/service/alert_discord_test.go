package service

import (
	"errors"
	"testing"
)

// Discord takes the webhooks of its hosts, over https, and lists show the
// host and the end of the URL.
func TestDiscordDestination(t *testing.T) {
	for _, bad := range []string{"https://discord.com/channels/1", "http://discord.com/api/webhooks/1/abc", "https://example.com/api/webhooks/1/abc"} {
		if _, err := (discordDestination{}).check(AlertDestinationInput{URL: bad}); !errors.Is(err, ErrAlertURL) {
			t.Errorf("check(%q) = %v, want ErrAlertURL", bad, err)
		}
	}
	for _, good := range []string{"https://discord.com/api/webhooks/1/abcd", "https://discordapp.com/api/webhooks/1/abcd", "https://canary.discord.com/api/webhooks/1/abcd"} {
		if _, err := (discordDestination{}).check(AlertDestinationInput{URL: good}); err != nil {
			t.Errorf("check(%q) = %v", good, err)
		}
	}
	to, _ := discordDestination{}.check(AlertDestinationInput{URL: "https://discord.com/api/webhooks/1/abcdWXYZ"})
	if hint := (discordDestination{}).hint(to); hint != "discord.com/…WXYZ" {
		t.Errorf("hint = %q", hint)
	}
}
