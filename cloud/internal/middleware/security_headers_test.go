package middleware

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
)

// Only scripts with the nonce of the page run, and nothing frames it.
func TestPolicyRunsOnlyThePagesScripts(t *testing.T) {
	policy := contentSecurityPolicy(&config.Config{RenderConfig: config.RenderConfig{UmamiHost: "umami.example.com"}}, "abc")
	for _, want := range []string{"script-src 'self' 'nonce-abc' https://umami.example.com", "connect-src 'self' https://umami.example.com", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, want) {
			t.Errorf("policy misses %q: %s", want, policy)
		}
	}
}
