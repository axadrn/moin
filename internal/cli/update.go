package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// version prints the version of this binary and, when signed in, of the
// hub, and says when a newer release is out.
func version(ctx context.Context, args []string, stdout io.Writer) error {
	if err := noArguments("version", args); err != nil {
		return err
	}
	fmt.Fprintln(stdout, build.Name, build.Version)
	config, err := client.LoadConfig()
	if err != nil {
		return nil
	}
	api, err := client.New(config)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := api.Version(ctx)
	if err != nil {
		return nil
	}
	fmt.Fprintln(stdout, "hub", info.Version, style.Muted.Render(config.URL))
	if info.HubBackup != nil {
		fmt.Fprintln(stdout, "hub backup", hubBackupLine(*info.HubBackup, time.Now()))
	}
	printUpdate(stdout, info)
	return nil
}

// hubBackupLine is how the backup of the hub's own database stands, like
// "● on · bucket acme-hub · last copy just now", red when off or late.
func hubBackupLine(backup client.HubBackup, now time.Time) string {
	status := style.StatusLive
	if backup.State != "on" {
		status = style.StatusFailed
	}
	line := backup.State
	if backup.Bucket != "" {
		line += " · bucket " + backup.Bucket
	}
	if backup.LastCopy != nil {
		line += " · last copy " + human.Ago(*backup.LastCopy, now)
	}
	return style.Badge(status, line)
}

// printUpdate says that a self-hosted hub has a newer release and how
// to update it: on its server.
func printUpdate(stdout io.Writer, info client.Version) {
	if !info.UpdateAvailable {
		return
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, style.Warning.Render(build.Name+" "+info.Latest+" is out.")+" "+info.Summary)
	if info.NotesURL != "" {
		fmt.Fprintln(stdout, "  "+style.Muted.Render(info.NotesURL))
	}
	if info.ReadFirst != "" {
		fmt.Fprintln(stdout, "  "+info.ReadFirst)
	}
	hint(stdout, "Then on the server:", "sh /opt/moin/update.sh")
}

// update updates an app from the catalog.
func update(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("update", "update <app> [--version <tag>]")
	appVersion := flags.String("version", "", "a version from moin catalog; default the newest")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%w. A self-hosted hub updates on its server: `sh /opt/moin/update.sh`", flags.wrong())
	}
	return updateApp(ctx, flags.Arg(0), *appVersion, stdout)
}

// daemonUpdates shows or sets whether the daemons update by themselves.
func daemonUpdates(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("servers auto-update", "servers auto-update [on|off]")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if flags.NArg() == 0 {
		on, err := api.DaemonUpdates(ctx)
		if err != nil {
			return err
		}
		if on {
			fmt.Fprintln(stdout, "Daemons update to the hub's version by themselves: on")
			hint(stdout, "Turn it off with", build.Name+" servers auto-update off")
		} else {
			fmt.Fprintln(stdout, "Daemons update to the hub's version by themselves: off")
			hint(stdout, "Update them by hand with", build.Name+" servers update")
		}
		return nil
	}
	var on bool
	switch flags.Arg(0) {
	case "on":
		on = true
	case "off":
	default:
		return flags.wrong()
	}
	if err := api.SetDaemonUpdates(ctx, on); err != nil {
		return err
	}
	if on {
		done(stdout, "Daemons update by themselves now, one server at a time")
	} else {
		done(stdout, "Daemons no longer update by themselves. Update them with %s", build.Name+" servers update")
	}
	return nil
}
