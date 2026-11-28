package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNoSecretInDeploys: a deploy carries no secret.
func TestNoSecretInDeploys(t *testing.T) {
	body, _ := json.Marshal(DeployStart{App: "shop", Image: "ghcr.io/me/shop:1"})
	for _, word := range []string{"s3cret", "secret_key", "env\"", "registry", "tunnel_token", "tailscale_key"} {
		if strings.Contains(string(body), word) {
			t.Errorf("a deploy carries %s: %s", word, body)
		}
	}
}
