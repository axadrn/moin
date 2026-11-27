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
