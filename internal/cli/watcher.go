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

// watcherCommand runs the watcher, or prints the token of a new server for
// its config.
func watcherCommand(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "add" {
		return watcherAdd(args[1:], stdout)
	}
	flags := newFlags("watcher", "watcher [--listen :8080] [--config "+watcher.DefaultConfig+"] [--dir "+watcher.DefaultDir+"]")
	listen := flags.String("listen", ":8080", "the address it listens on")
	config := flags.String("config", watcher.DefaultConfig, "the config file")
	dir := flags.String("dir", watcher.DefaultDir, "where it keeps what it knows")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unknown command %q. Run `%s help`", "watcher "+flags.Arg(0), build.Name)
	}
	return watcher.Run(ctx, *listen, *config, *dir)
}

// watcherAdd prints a new token for a server: the line for the watcher's
// config, and the command that points the server at this watcher.
func watcherAdd(args []string, stdout io.Writer) error {
	flags := newFlags("watcher add", "watcher add <server> [--url https://watcher.example.com] [--config "+watcher.DefaultConfig+"]")
	address := flags.String("url", "https://<this watcher>", "the address daemons reach this watcher at")
	path := flags.String("config", watcher.DefaultConfig, "the config file")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return flags.wrong()
	}
	name := flags.Arg(0)
	if err := watcher.CheckName(name); err != nil {
		return err
	}
	config, err := watcher.LoadConfig(*path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, ok := config.Servers[name]; ok {
		return fmt.Errorf("%s is in %s already. Remove its line first for a new token", name, *path)
	}
	token := watcher.NewToken()
	fmt.Fprintf(stdout, `1. Put this line under servers: in %s, then restart the watcher:

  %s: %s

2. On your laptop, point %s at this watcher:

  %s watchers add %s %s %s
`, *path, name, token, name, build.Name, name, *address, token)
	return nil
}
