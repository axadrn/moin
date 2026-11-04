package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/style"
)

// execArgs splits `exec [app] [flags] -- <command…>` at the first "--":
// what comes before is ops's, what comes after is the command's own, flags
// and all.
func execArgs(args []string) (own, command []string) {
	cut := slices.Index(args, "--")
	if cut < 0 {
		return args, nil
	}
	return args[:cut], args[cut+1:]
}

// execCommand runs one command in a container of an app on its server, and
// prints what it writes as it comes. It exits with the command's code.
func execCommand(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("exec", "exec [app] [--service web] [--timeout 10m] -- <command> [args]").withApp()
	service := flags.String("service", "", "the compose service to run it in; default the app's main service")
	timeout := flags.Duration("timeout", 10*time.Minute, "how long to wait for it, at most 1h")
	own, command := execArgs(args)
	if err := flags.parse(own); err != nil {
		return err
	}
	if flags.NArg() > 1 || len(command) == 0 {
		return flags.wrong()
	}
	if *timeout <= 0 || *timeout > time.Hour {
		return fmt.Errorf("--timeout is between 1s and 1h. Usage: `%s`", flags.usage)
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	result, err := api.Exec(ctx, name, *service, command, "", int(timeout.Seconds()), func(piece string) {
		fmt.Fprint(stdout, piece)
	})
	if err != nil {
		return err
	}
	switch {
	case result.TimedOut:
		fmt.Fprintln(os.Stderr, style.Dim.Render(fmt.Sprintf("%s stopped waiting after %s; the command may still run in %s. Stop it with `%s restart %s`.", build.Name, timeout, result.Service, build.Name, name)))
	case result.Truncated:
		fmt.Fprintln(os.Stderr, style.Dim.Render("The output was cut at 1 MB."))
	}
	if result.TimedOut {
		os.Exit(124)
	}
	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}
	return nil
}
