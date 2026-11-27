package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// A Cloudflare Tunnel brings the web to the server from Cloudflare:
// cloudflared dials out, and ports 80 and 443 can close.

const (
	tunnelImage      = "cloudflare/cloudflared:2026.9.3"
	tunnelEntrypoint = "tunnel"
	tunnelPort       = "8080"
	// tunnelWait is how long cloudflared has to reach Cloudflare before
	// moin gives up on a new token.
	tunnelWait = 45 * time.Second
)

// ensureTunnel runs cloudflared with the token, or removes it without one.
func (s *security) ensureTunnel(ctx context.Context, token string) error {
	running, err := s.apps.output(ctx, "docker", "inspect", "-f", "{{.State.Running}} {{.Config.Image}} {{index .Config.Labels \""+labelToken+"\"}}", tunnelName)
	want := "true " + tunnelImage + " " + tokenLabel(token)
	if err == nil && strings.TrimSpace(running) == want && token != "" {
		return nil
	}
	if err == nil {
		_, _ = s.apps.output(ctx, "docker", "rm", "-f", tunnelName)
	}
	if token == "" {
		return nil
	}
	// "-e TUNNEL_TOKEN" without a value makes docker take it from its own
	// environment, so the token never shows on a command line in ps.
	_, err = s.apps.outputEnv(ctx, []string{"TUNNEL_TOKEN=" + token}, "docker", "run", "-d", "--name", tunnelName, "--restart", "unless-stopped",
		"--network", proxyNetwork,
		"--label", labelToken+"="+tokenLabel(token),
		"-e", "TUNNEL_TOKEN",
		tunnelImage, "tunnel", "--no-autoupdate", "run")
	if err != nil {
		return fmt.Errorf("Cloudflare Tunnel: %w", err)
	}
	return nil
}

// waitTunnel waits until cloudflared says it reached Cloudflare.
func waitTunnel(ctx context.Context) error {
	deadline := time.Now().Add(tunnelWait)
	for time.Now().Before(deadline) {
		// cloudflared logs on stderr.
		logs, _ := exec.CommandContext(ctx, "docker", "logs", tunnelName).CombinedOutput()
		if strings.Contains(string(logs), "Registered tunnel connection") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return errors.New("cloudflared did not reach Cloudflare with that token, so the web stays where it is. See the tunnel in your Cloudflare dashboard")
}

// tokenLabel tells tunnel tokens apart without storing them in a label:
// the start of a hash.
func tokenLabel(token string) string {
	if token == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(token)))[:12]
}

// validTunnelToken says whether token looks like a Cloudflare Tunnel
// token: base64 of JSON that names the tunnel in "t".
func validTunnelToken(token string) bool {
	body, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		body, err = base64.RawStdEncoding.DecodeString(token)
	}
	var fields struct {
		Tunnel string `json:"t"`
	}
	return err == nil && json.Unmarshal(body, &fields) == nil && fields.Tunnel != ""
}
