package cli

import (
	"context"
	"io"
	"os"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
)

// compose is `moin compose [app]`: the compose file moin runs for the app,
// with the proxy and without a value of the env. It goes to stdout as it
// is, so `moin compose my-app > compose.yml` saves it, to run the app
// anywhere.
func compose(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("compose", "compose [app] [-s server] [--json]").withServer().withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		file, err := appCompose(ctx, server, app, os.Stderr)
		if err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, file)
		}
		if _, err := io.WriteString(raw(stdout), file.Compose); err != nil {
			return err
		}
		if isTTY(stdout) {
			hint(stdout, "Save it with", build.Name+" compose "+app+" > compose.yml")
		}
		return nil
	})
}

// appCompose asks server for the compose file of the app's live release.
func appCompose(ctx context.Context, server, app string, said io.Writer) (daemon.Compose, error) {
	var file daemon.Compose
	err := sshAnswer(ctx, server, []string{"compose", app}, nil, said, &file)
	return file, err
}
