package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
)

// updateWait is how long `ops update` follows an update.
const updateWait = 5 * time.Minute

// version prints the version of this binary and, when signed in, of the
// control plane, and says when a newer release is out.
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
	fmt.Fprintln(stdout, "control plane", info.Version, style.Muted.Render(config.URL))
	printUpdate(stdout, info)
	return nil
}

// printUpdate says how a self-hosted control plane gets the newest release.
func printUpdate(stdout io.Writer, info client.Version) {
	switch {
	case info.Update.Running():
		fmt.Fprintln(stdout)
		step(stdout, "Updating the control plane to %s", info.Update.Version)
	case info.UpdateAvailable:
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, style.Warning.Render(build.Name+" "+info.Latest+" is out.")+" "+info.Summary)
		if info.NotesURL != "" {
			fmt.Fprintln(stdout, "  "+style.Muted.Render(info.NotesURL))
		}
		switch {
		case info.CanUpdate:
			hint(stdout, "Update the control plane with", build.Name+" update")
		case info.Updater:
			hint(stdout, "An admin of the control plane updates it, with", build.Name+" update")
		default:
			hint(stdout, "Update it on the server with", "sh update.sh")
		}
	}
}

// update updates a self-hosted control plane to the newest release and
// follows it until it is back.
func update(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("update", "update [--yes], or update <app> [--version <tag>] for an app from the catalog").withYes()
	appVersion := flags.String("version", "", "with an app: a release from ops catalog; default the newest")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() == 1 {
		return updateApp(ctx, flags.Arg(0), *appVersion, stdout)
	}
	if flags.NArg() > 1 || *appVersion != "" {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	info, err := api.Version(ctx)
	if err != nil {
		return err
	}
	if !info.SelfHosted {
		done(stdout, "This control plane runs %s and updates itself with every release. Nothing to do.", info.Version)
		return nil
	}
	if !info.UpdateAvailable && !info.Update.Running() {
		done(stdout, "The control plane runs %s, the newest release.", info.Version)
		return nil
	}
	if !info.Update.Running() {
		fmt.Fprintln(stdout, style.Bold.Render(build.Name+" "+info.Latest)+" "+info.Summary)
		if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Update the control plane from %s to %s? It is away for a minute; your apps keep running.", info.Version, info.Latest)); err != nil {
			return err
		}
		info, err = api.UpdateControlPlane(ctx)
		var apiErr *client.Error
		if errors.As(err, &apiErr) && apiErr.Code == "no_updater" {
			return fmt.Errorf("%s", apiErr.Message)
		}
		if err != nil {
			return err
		}
	}
	target := info.Latest
	if info.Update != nil {
		target = info.Update.Version
	}
	progress := newProgress(stdout, "updating the control plane to "+target)
	deadline := time.Now().Add(updateWait)
	for {
		select {
		case <-ctx.Done():
			progress.end()
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
		// The control plane is away while it restarts.
		current, err := api.Version(ctx)
		if err == nil && current.Update != nil && !current.Update.Running() {
			progress.end()
			switch current.Update.State {
			case "done":
				done(stdout, "The control plane runs %s now", style.Bold.Render(current.Version))
				return nil
			case "rolled_back":
				return fmt.Errorf("the update to %s did not work, so the control plane runs %s again: %s", current.Update.Version, current.Version, current.Update.Reason)
			default:
				return fmt.Errorf("the update to %s did not work: %s", current.Update.Version, current.Update.Reason)
			}
		}
		if time.Now().After(deadline) {
			progress.end()
			return fmt.Errorf("the update to %s has not ended after %s: see `docker compose logs updater` on the server", target, updateWait)
		}
	}
}

// agentUpdates shows or sets whether the agents update by themselves.
func agentUpdates(ctx context.Context, args []string, stdout io.Writer) error {
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
		on, err := api.AgentUpdates(ctx)
		if err != nil {
			return err
		}
		if on {
			fmt.Fprintln(stdout, "Agents update to the control plane's version by themselves: on")
			hint(stdout, "Turn it off with", build.Name+" servers auto-update off")
		} else {
			fmt.Fprintln(stdout, "Agents update to the control plane's version by themselves: off")
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
	if err := api.SetAgentUpdates(ctx, on); err != nil {
		return err
	}
	if on {
		done(stdout, "Agents update by themselves now, one server at a time")
	} else {
		done(stdout, "Agents no longer update by themselves. Update them with %s", build.Name+" servers update")
	}
	return nil
}
