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

// watcherAdd prints a new token for a server: one line for the watcher's
// config, and what the server's config needs to report to it.
func watcherAdd(args []string, stdout io.Writer) error {
	flags := newFlags("watcher add", "watcher add <server> [--config "+watcher.DefaultConfig+"]")
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
	fmt.Fprintf(stdout, `Put this line under servers: in %s, then restart the watcher:

  %s: %s

On %s, add this watcher to /etc/%s/config.yml:

watchers:
  - url: https://<this watcher>
    token: %s
`, *path, name, token, name, build.Name, token)
	return nil
}
