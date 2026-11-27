package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/watcher"
)

// watcherCommand is `moin watcher run` and `moin watcher token`, the two
// commands of the machine that runs a watcher.
func watcherCommand(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "run" {
		return watcherRun(ctx, args[1:])
	}
	if len(args) > 0 && args[0] == "token" {
		return watcherToken(args[1:], stdout)
	}
	return fmt.Errorf("usage: `%s watcher run`, `%s watcher token <server> --url <url>`, `%s watcher connect <server> <url> <token>` or `%s watcher disconnect <server> <url>`", build.Name, build.Name, build.Name, build.Name)
}

// watcherRun runs the watcher until it is stopped.
func watcherRun(ctx context.Context, args []string) error {
	flags := newFlags("watcher run", "watcher run [--listen :8080] [--config "+watcher.DefaultConfig+"] [--dir "+watcher.DefaultDir+"]")
	listen := flags.String("listen", ":8080", "the address it listens on")
	config := flags.String("config", watcher.DefaultConfig, "the config file")
	dir := flags.String("dir", watcher.DefaultDir, "where it keeps what it knows")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	return watcher.Run(ctx, *listen, *config, *dir)
}

// watcherToken prints a new token for a server: the line for the watcher's
// config, or a whole starter config when there is none yet, and the
// command that points the server at this watcher.
func watcherToken(args []string, stdout io.Writer) error {
	flags := newFlags("watcher token", "watcher token <server> --url https://watcher.example.com [--config "+watcher.DefaultConfig+"]")
	address := flags.String("url", "", "the address daemons reach this watcher at")
	path := flags.String("config", watcher.DefaultConfig, "the config file")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || *address == "" {
		return flags.wrong()
	}
	name := flags.Arg(0)
	if err := watcher.CheckName(name); err != nil {
		return err
	}
	config, err := watcher.LoadConfig(*path)
	fresh := errors.Is(err, fs.ErrNotExist)
	if err != nil && !fresh {
		return err
	}
	if _, ok := config.Servers[name]; ok {
		return fmt.Errorf("%s is in %s already. Remove its line first for a new token", name, *path)
	}
	token := watcher.NewToken()
	if fresh {
		fmt.Fprintf(stdout, `1. Save this as %s, fill in alerts:, then run: chmod 600 %s

password: %s
servers:
  %s: %s
alerts:
  - kind: telegram
    token: <bot token>
    chat_id: <chat id>

`, *path, *path, watcher.NewToken(), name, token)
	} else {
		fmt.Fprintf(stdout, `1. Put this line under servers: in %s, then restart the watcher:

  %s: %s

`, *path, name, token)
	}
	fmt.Fprintf(stdout, `2. On your laptop, point %s at this watcher:

  %s watcher connect %s %s %s
`, name, build.Name, name, *address, token)
	return nil
}
