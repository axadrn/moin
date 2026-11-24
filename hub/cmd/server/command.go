package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/axadrn/moin/hub/internal/app"
	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/service"
)

// command runs a one-off task of the operator, such as from install.sh:
//
//	bootstrap <server>     the account of the first ADMIN_EMAILS address, a
//	                       workspace and the server, named like this
//	                       machine; prints its install token, or nothing
//	                       when its daemon connected already
//	recovery-key <email>   prints a key that signs in to the account of
//	                       email once, within an hour, signs it out
//	                       everywhere else and disconnects its GitHub: the
//	                       first sign-in of a self-hosted hub, or a way back
//	                       for someone who lost their GitHub account
func command(cfg *config.Config, args []string) error {
	switch args[0] {
	case "bootstrap":
		if len(args) != 2 {
			return errors.New("usage: bootstrap <server name>")
		}
		if len(cfg.AdminEmails) == 0 {
			return errors.New("set ADMIN_EMAILS: the first address is the account bootstrap makes")
		}
		application, err := app.New(cfg)
		if err != nil {
			return err
		}
		defer application.Close()
		token, err := service.Bootstrap(application.AuthService, application.WorkspaceService, application.ServerService, cfg.AdminEmails[0], args[1])
		if err != nil {
			return err
		}
		if token != "" {
			fmt.Println(token)
		}
		return nil
	case "recovery-key":
		if len(args) != 2 {
			return errors.New("usage: recovery-key <email>")
		}
		application, err := app.New(cfg)
		if err != nil {
			return err
		}
		defer application.Close()
		key, err := application.AuthService.CreateRecoveryKey(args[1])
		if err != nil {
			return err
		}
		fmt.Println(key)
		fmt.Println("Enter it at " + strings.TrimRight(cfg.AppURL, "/") + "/auth/key. It is valid for 1 hour and works once.")
		fmt.Println("It signs out everywhere and disconnects GitHub: connect your GitHub again under Settings, Account, Security.")
		return nil
	}
	return fmt.Errorf("unknown command %q: bootstrap <server name> or recovery-key <email>", args[0])
}
