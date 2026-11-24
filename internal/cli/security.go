package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
	"github.com/axadrn/moin/protocol"
)

// secure sets how a server is reached; flags it does not get keep their
// value.
func secure(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("servers secure", "servers secure <server> [--tunnel on|off] [--ssh tailscale|everyone|off] [--firewall on|off] [--yes]").withYes()
	tunnel := flags.String("tunnel", "", "on: visitors come through a Cloudflare Tunnel, no open port; off: ports 80 and 443")
	ssh := flags.String("ssh", "", "tailscale: SSH only through Tailscale; everyone; off: the daemon stops sshd")
	firewall := flags.String("firewall", "", "on: drop every other inbound connection")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	servers, err := api.Servers(ctx)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(servers, func(s client.Server) bool { return s.Name == flags.Arg(0) || s.ID == flags.Arg(0) })
	if index < 0 {
		return fmt.Errorf("no server %s", flags.Arg(0))
	}
	server := servers[index]
	now := client.Security{Tunnel: server.Tunnel, SSH: server.SSH, Firewall: server.Firewall}
	security := now
	for _, setting := range []struct {
		value     string
		target    *bool
		flagLabel string
	}{
		{*tunnel, &security.Tunnel, "--tunnel"},
		{*firewall, &security.Firewall, "--firewall"},
	} {
		switch setting.value {
		case "":
		case "on":
			*setting.target = true
		case "off":
			*setting.target = false
		default:
			return fmt.Errorf("%s is on or off", setting.flagLabel)
		}
	}
	switch *ssh {
	case "":
	case protocol.SSHTailscale, protocol.SSHEveryone, protocol.SSHOff:
		security.SSH = *ssh
	default:
		return errors.New("--ssh is tailscale, everyone or off")
	}
	if security != now {
		next := server
		next.Tunnel, next.SSH, next.Firewall = security.Tunnel, security.SSH, security.Firewall
		question := fmt.Sprintf("Change how %s is reached from %s to %s?", server.Name, server.Access(), next.Access())
		if security.SSH != now.SSH && security.SSH != protocol.SSHEveryone {
			question += " Keep another way in, like your provider's console."
		}
		err = confirm(stdin, stdout, flags.yes(), question)
		if err != nil {
			return err
		}
	}
	step(stdout, "applying to %s", server.Name)
	secured, err := api.SetSecurity(ctx, server.ID, security)
	if err != nil {
		return err
	}
	printSecurity(stdout, secured)
	return nil
}

// printSecurity says what is on the server, line by line; a line that is
// not as set says both.
func printSecurity(w io.Writer, server client.Server) {
	lines := server.SecurityLines()
	if lines == nil {
		done(w, "%s: %s. Its daemon applies it when it connects.", server.Name, server.Access())
		return
	}
	done(w, "%s: %s", server.Name, server.Access())
	report := server.Security
	for _, line := range lines {
		value := line.Is
		switch {
		case line.Is != line.Set:
			value = style.Danger.Render(line.Is + " on the server ✗ " + build.Name + " says " + line.Set)
		case line.Name == "ssh" && report.TailscaleIP != "":
			value += " · " + report.TailscaleIP
		case line.Name == "firewall" && report.Firewall:
			value += " · open to everyone: " + report.OpenText()
		}
		fmt.Fprintln(w, style.Muted.Render(fmt.Sprintf("  %-10s", line.Name))+value)
	}
	for _, problem := range report.Problems {
		fmt.Fprintln(w, "  "+style.Warning.Render("! "+problem))
	}
}

// integrations shows or stores the Cloudflare token, which the hub keeps,
// and hands a server the Tailscale key, which only the server keeps.
func integrations(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	// Help works without a login.
	api, apiErr := apiClient()
	flags := newFlags("integrations", "integrations, integrations cloudflare|tailscale [--server <server>] with the key on stdin, or integrations rm cloudflare|tailscale [--server <server>] [--yes]").withYes()
	server := flags.String("server", "", "the server that keeps the Tailscale key; needed with more than one")
	if err := flags.parse(args); err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	args = flags.Args()
	if len(args) == 0 {
		stored, err := api.Integrations(ctx)
		if err != nil {
			return err
		}
		cloudflare, tailscale := "not set", "not set"
		if stored.Cloudflare {
			cloudflare = "set, account " + stored.CloudflareAccount
		}
		if len(stored.Tailscale.Servers) > 0 {
			tailscale = "kept on " + keptOn(stored.Tailscale.Servers, stored.Tailscale.MissingOn)
		}
		table(stdout, []string{"KEY", "STATE"}, [][]string{{"cloudflare", cloudflare}, {"tailscale", tailscale}})
		return nil
	}
	remove := args[0] == "rm"
	if remove {
		args = args[1:]
	}
	if len(args) != 1 || (args[0] != "cloudflare" && args[0] != "tailscale") {
		return flags.wrong()
	}
	value := ""
	var err error
	if remove {
		stored, err := api.Integrations(ctx)
		if err != nil {
			return err
		}
		if (args[0] == "cloudflare" && !stored.Cloudflare) || (args[0] == "tailscale" && len(stored.Tailscale.Servers) == 0) {
			done(stdout, "Nothing changed: no %s key is stored.", args[0])
			return nil
		}
		err = confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Remove the %s key?", args[0]))
		if err != nil {
			return err
		}
	} else {
		if isTerminal(stdin) {
			fmt.Fprintf(stdout, "%s key: ", args[0])
		}
		value, err = readSecret(stdin)
		if isTerminal(stdin) {
			fmt.Fprintln(stdout)
		}
		if err != nil {
			return err
		}
		if value == "" {
			return errors.New("the key is empty")
		}
	}
	var cloudflare, tailscale *string
	if args[0] == "cloudflare" {
		cloudflare = &value
	} else {
		tailscale = &value
	}
	_, err = api.SetIntegrations(ctx, cloudflare, tailscale, *server)
	if err != nil {
		return err
	}
	switch {
	case remove:
		done(stdout, "Removed the %s key.", args[0])
	case args[0] == "tailscale":
		done(stdout, "The server keeps the Tailscale key now; the hub does not.")
	default:
		done(stdout, "Stored the Cloudflare token, encrypted. The hub uses it to make tunnels and DNS records.")
	}
	return nil
}
