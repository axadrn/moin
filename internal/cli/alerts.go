package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/axadrn/ops/internal/build"
)

// alerts shows or sets whether the signed-in user gets email alerts.
func alerts(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("alerts", "alerts [on|off]")
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
		account, err := api.Account(ctx)
		if err != nil {
			return err
		}
		if account.EmailAlerts {
			fmt.Fprintln(stdout, "Email alerts to "+account.Email+": on")
			hint(stdout, "Turn them off with", build.Name+" alerts off")
		} else {
			fmt.Fprintln(stdout, "Email alerts to "+account.Email+": off")
			hint(stdout, "Turn them on with", build.Name+" alerts on")
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
	if err := api.SetEmailAlerts(ctx, on); err != nil {
		return err
	}
	if on {
		done(stdout, "Email alerts on: a mail when a site goes down or comes back, a server goes offline or a deploy fails")
	} else {
		done(stdout, "Email alerts off")
	}
	return nil
}
