package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
	"golang.org/x/term"
)

// secure sets how a server is reached; flags it does not get keep their
// value.
func secure(ctx context.Context, api *client.Client, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("servers secure", flag.ContinueOnError)
	tunnel := flags.String("tunnel", "", "on: app traffic through a Cloudflare Tunnel, no open port; off: ports 80 and 443")
	ssh := flags.String("ssh", "", "tailscale: SSH only on the tailnet; open: everywhere")
	firewall := flags.String("firewall", "", "on: drop every other inbound connection")
	if err := parseAnywhere(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s servers secure <server> [--tunnel on|off] [--ssh tailscale|open] [--firewall on|off]", build.Name)
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
	api, err := apiClient()
	if err != nil {
		return err
	}
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
		return fmt.Errorf("usage: %s integrations [rm] cloudflare|tailscale; the key comes on stdin", build.Name)
	}
	value := ""
	if !remove {
		if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			fmt.Fprintf(stdout, "%s key: ", args[0])
		}
		value, err = readSecret(stdin)
		if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
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
