// Command moin opens the TUI without arguments, runs a CLI command with them,
// and runs the daemon on a server with `moin daemon`.
package main

import (
	"os"

	"github.com/axadrn/moin/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
