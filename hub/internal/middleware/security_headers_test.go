package middleware

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/brand"
)

// The home page plays its video from the media bucket; without media-src
// the browser blocks it and shows an empty player.
func TestPolicyLetsPagesPlayMedia(t *testing.T) {
	policy := contentSecurityPolicy(SecurityHeadersConfig{}, "nonce")
	if !strings.Contains(policy, "media-src 'self' "+brand.Media) {
		t.Fatalf("media-src misses %s: %s", brand.Media, policy)
	}
}

// The form that creates the hub's GitHub App posts to GitHub; without it
// in form-action the browser refuses to send it.
func TestPolicyLetsTheGitHubAppFormPost(t *testing.T) {
	policy := contentSecurityPolicy(SecurityHeadersConfig{GitHubURL: "https://github.com"}, "nonce")
	if !strings.Contains(policy, "form-action 'self' https://sandbox.polar.sh https://polar.sh https://checkout.stripe.com https://github.com") {
		t.Fatalf("form-action misses GitHub: %s", policy)
	}
}
