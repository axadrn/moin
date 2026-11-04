package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
)

// secure sets how a server is reached; flags it does not get keep their
// value.
func secure(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("servers secure", "servers secure <server> [--tunnel on|off] [--ssh tailscale|open] [--firewall on|off] [--yes]").withYes()
	tunnel := flags.String("tunnel", "", "on: app traffic through a Cloudflare Tunnel, no open port; off: ports 80 and 443")
	ssh := flags.String("ssh", "", "tailscale: SSH only on the tailnet; open: everywhere")
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
	security := client.Security{Tunnel: server.Tunnel, SSHTailscale: server.SSHTailscale, Firewall: server.Firewall}
	for _, setting := range []struct {
		value     string
		on, off   string
		target    *bool
		flagLabel string
	}{
		{*tunnel, "on", "off", &security.Tunnel, "--tunnel"},
		{*ssh, "tailscale", "open", &security.SSHTailscale, "--ssh"},
		{*firewall, "on", "off", &security.Firewall, "--firewall"},
	} {
		switch setting.value {
		case "":
		case setting.on:
			*setting.target = true
		case setting.off:
			*setting.target = false
		default:
			return fmt.Errorf("%s is %s or %s", setting.flagLabel, setting.on, setting.off)
		}
	}
	if security != (client.Security{Tunnel: server.Tunnel, SSHTailscale: server.SSHTailscale, Firewall: server.Firewall}) {
		next := server
		next.Tunnel, next.SSHTailscale, next.Firewall = security.Tunnel, security.SSHTailscale, security.Firewall
		question := fmt.Sprintf("Change how %s is reached from %s to %s?", server.Name, server.Access(), next.Access())
		if security.SSHTailscale && !server.SSHTailscale {
			question += " Port 22 closes once Tailscale is up; keep another way in."
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

func printSecurity(w io.Writer, server client.Server) {
	report := server.Security
	if report == nil {
		done(w, "%s: %s. Its agent applies it when it connects.", server.Name, server.Access())
		return
	}
	done(w, "%s: %s", server.Name, server.Access())
	open := "no firewall"
	if report.Firewall {
		open = strings.Join(report.OpenPorts, ", ")
		if open == "" {
			open = "nothing"
		}
	}
	fmt.Fprintln(w, style.Muted.Render("  open to everyone: ")+open)
	if report.TailscaleIP != "" {
		fmt.Fprintln(w, style.Muted.Render("  tailnet: ")+report.TailscaleIP)
	}
	for _, problem := range report.Problems {
		fmt.Fprintln(w, "  "+style.Warning.Render("! "+problem))
	}
}

// integrations shows or stores the Cloudflare token and the Tailscale key.
func integrations(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	// Help works without a login.
	api, apiErr := apiClient()
	flags := newFlags("integrations", "integrations, integrations cloudflare|tailscale with the key on stdin, or integrations rm cloudflare|tailscale [--yes]").withYes()
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
		if stored.Tailscale {
			tailscale = "set"
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
		if (args[0] == "cloudflare" && !stored.Cloudflare) || (args[0] == "tailscale" && !stored.Tailscale) {
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
	_, err = api.SetIntegrations(ctx, cloudflare, tailscale)
	if err != nil {
		return err
	}
	if remove {
		done(stdout, "Removed the %s key.", args[0])
	} else {
		done(stdout, "Stored the %s key, encrypted.", args[0])
	}
	return nil
}
