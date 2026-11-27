package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNoSecretInDeploysAndConfigs: a deploy and a server config carry no
// secret.
func TestNoSecretInDeploysAndConfigs(t *testing.T) {
	for _, data := range []any{
		DeployStart{App: "shop", Image: "ghcr.io/me/shop:1"},
		ServerConfig{SSH: SSHTailscale, Firewall: true},
	} {
		body, _ := json.Marshal(data)
		for _, word := range []string{"s3cret", "secret_key", "env\"", "registry", "tunnel_token", "tailscale_key"} {
			if strings.Contains(string(body), word) {
				t.Errorf("%T carries %s: %s", data, word, body)
			}
		}
	}
}

func TestSecretNames(t *testing.T) {
	for name, want := range map[string][2]string{
		RegistrySecret("ghcr.io"): {"registry", "ghcr.io"},
		SecretTailscale:           {"tailscale", ""},
	} {
		if kind, of := SecretOf(name); kind != want[0] || of != want[1] {
			t.Errorf("SecretOf(%q) = %q, %q", name, kind, of)
		}
	}
	if RegistryHost("ghcr.io/me/shop:1") != "ghcr.io" || RegistryHost("me/shop") != "docker.io" || RegistryHost("localhost:5000/shop") != "localhost:5000" {
		t.Error("RegistryHost")
	}
}
