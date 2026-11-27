package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/alert"
)

// Tailscale puts the server in your tailnet, your own private network.
// With public port 22 closed, SSH comes only through it.

// keyExpiryWarning is how long before the server's Tailscale key expires
// moin says so: once it has, SSH through the tailnet stops.
const keyExpiryWarning = 14 * 24 * time.Hour

// Tailnet is this server in a tailnet: its address and its name there,
// and when its key expires. All are empty when it is in none.
type Tailnet struct {
	IP        string    `json:"ip"`
	Name      string    `json:"name"`
	KeyExpiry time.Time `json:"key_expiry,omitzero"`
}

// tailscaleStatus is what `tailscale status --json` says: Tailscale's
// state, like Running or NeedsLogin, and the server in the tailnet. The
// state is "" without Tailscale.
func tailscaleStatus(ctx context.Context) (string, Tailnet) {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return "", Tailnet{}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return "", Tailnet{}
	}
	return parseTailscaleStatus(body)
}

func parseTailscaleStatus(body []byte) (string, Tailnet) {
	var status struct {
		BackendState string
		Self         struct {
			DNSName      string
			TailscaleIPs []string
			KeyExpiry    *time.Time
		}
	}
	if json.Unmarshal(body, &status) != nil {
		return "", Tailnet{}
	}
	if status.BackendState != "Running" {
		return status.BackendState, Tailnet{}
	}
	tailnet := Tailnet{Name: strings.TrimSuffix(status.Self.DNSName, ".")}
	if status.Self.KeyExpiry != nil {
		tailnet.KeyExpiry = *status.Self.KeyExpiry
	}
	for _, ip := range status.Self.TailscaleIPs {
		if !strings.Contains(ip, ":") {
			tailnet.IP = ip
			break
		}
	}
	return status.BackendState, tailnet
}

// TailnetHere asks Tailscale where this server is in the tailnet.
func TailnetHere(ctx context.Context) Tailnet {
	_, tailnet := tailscaleStatus(ctx)
	return tailnet
}

// tailnetNeed is one thing the tailnet of a server needs. key names it
// for its alert, title says what is wrong, text how to fix it, and
// command is where the fix starts.
type tailnetNeed struct{ key, title, text, command string }

// tailnetNeeds are what a server with SSH only through Tailscale needs:
// Tailscale running, and a key that does not expire soon. Without
// either, SSH to it stops.
func tailnetNeeds(ctx context.Context, dir string, now time.Time) []tailnetNeed {
	file, _ := loadServerFile(dir)
	if !file.SSHOnTailscale {
		return nil
	}
	state, tailnet := tailscaleStatus(ctx)
	return needsOf(state, tailnet, now)
}

// needsOf is tailnetNeeds for what Tailscale said.
func needsOf(state string, tailnet Tailnet, now time.Time) []tailnetNeed {
	if state != "Running" {
		return []tailnetNeed{{
			key:     "tailnet/down",
			title:   "Tailscale does not run, and SSH only comes through it",
			text:    "In your provider's web console, start it with `tailscale up`, or open port 22 with `" + installedPath + " unlock`.",
			command: "tailscale up",
		}}
	}
	if !tailnet.KeyExpiry.IsZero() && tailnet.KeyExpiry.Sub(now) < keyExpiryWarning {
		return []tailnetNeed{{
			key:     "tailnet/key",
			title:   "the Tailscale key expires on " + tailnet.KeyExpiry.Format("January 2") + ", then SSH through the tailnet stops",
			text:    KeyExpiryHint(tailnet.Name),
			command: tailscaleMachines,
		}}
	}
	return nil
}

// tailscaleMachines is the page of the Tailscale admin console that
// lists the machines of your tailnet.
const tailscaleMachines = "https://login.tailscale.com/admin/machines"

// KeyExpiryHint says how to turn off key expiry for the server called
// name in the tailnet.
func KeyExpiryHint(name string) string {
	machine, _, _ := strings.Cut(name, ".")
	if machine == "" {
		machine = "the server"
	}
	return "Turn off key expiry for it: Tailscale admin console, Machines, " + machine + ", the ⋯ menu, Disable key expiry."
}

// tailnetAlerts are the needs of the tailnet as alerts, by key.
func tailnetAlerts(ctx context.Context, dir, server string) map[string]alert.Message {
	alerts := map[string]alert.Message{}
	for _, need := range tailnetNeeds(ctx, dir, time.Now()) {
		alerts[need.key] = alert.Message{Server: server, Title: need.title, Text: need.text}
	}
	return alerts
}

// tailnetStatusNeeds are the needs of the tailnet, for `moin`.
func tailnetStatusNeeds(ctx context.Context, dir, server string) []Need {
	var needs []Need
	for _, need := range tailnetNeeds(ctx, dir, time.Now()) {
		needs = append(needs, Need{Text: server + ": " + need.title + ".", Command: need.command})
	}
	return needs
}

// JoinTailnetHere installs Tailscale when it is missing and brings it up,
// with authKey when there is one. Without one, Tailscale writes a link to
// log in to out and waits for it.
func JoinTailnetHere(ctx context.Context, authKey string, out io.Writer) (Tailnet, error) {
	if tailnet := TailnetHere(ctx); tailnet.IP != "" {
		return tailnet, nil
	}
	if _, err := exec.LookPath("tailscale"); err != nil {
		fmt.Fprintln(out, "installing Tailscale")
		install := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh")
		if output, err := install.CombinedOutput(); err != nil {
			return Tailnet{}, fmt.Errorf("install Tailscale: %s", lastLine(string(output)))
		}
	}
	args := []string{"up", "--timeout=5m"}
	if config, err := LoadConfig(); err == nil && config.Name != "" {
		args = append(args, "--hostname="+config.Name)
	}
	if authKey != "" {
		// Tailscale reads the key from a file with "file:", so the key
		// never shows on a command line in ps. Only root reads the file.
		keyFile, err := os.CreateTemp("", tempName("tailscale-key-*"))
		if err != nil {
			return Tailnet{}, err
		}
		defer os.Remove(keyFile.Name())
		_, err = keyFile.WriteString(authKey)
		if err = errors.Join(err, keyFile.Close()); err != nil {
			return Tailnet{}, err
		}
		args = append(args, "--auth-key=file:"+keyFile.Name())
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	var said bytes.Buffer
	up := exec.CommandContext(ctx, "tailscale", args...)
	up.Stdout, up.Stderr = io.MultiWriter(out, &said), io.MultiWriter(out, &said)
	if err := up.Run(); err != nil {
		return Tailnet{}, fmt.Errorf("tailscale up: %s", lastLine(said.String()))
	}
	tailnet := TailnetHere(ctx)
	if tailnet.IP == "" {
		return Tailnet{}, errors.New("Tailscale is up, but the server has no tailnet address")
	}
	return tailnet, nil
}
