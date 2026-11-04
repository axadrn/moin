package main

import (
	"errors"
	"fmt"

	"github.com/axadrn/ops/controlplane/internal/app"
	"github.com/axadrn/ops/controlplane/internal/config"
	"github.com/axadrn/ops/controlplane/internal/service"
)

// command runs a one-off task of the operator, such as from install.sh:
//
//	bootstrap <server>  the account of the first ADMIN_EMAILS address, a
//	                    workspace and the server, named like this machine;
//	                    prints its install token, or nothing when its agent
//	                    connected already
//	sign-in-link        prints a sign-in link for the first ADMIN_EMAILS
//	                    address, for a control plane without email
func command(cfg *config.Config, args []string) error {
	if len(cfg.AdminEmails) == 0 {
		return errors.New("set ADMIN_EMAILS: the first address is the account these commands are for")
	}
	email := cfg.AdminEmails[0]
	switch args[0] {
	case "bootstrap":
		if len(args) != 2 {
			return errors.New("usage: bootstrap <server name>")
		}
		application, err := app.New(cfg)
		if err != nil {
			return err
		}
		defer application.Close()
		token, err := service.Bootstrap(application.AuthService, application.WorkspaceService, application.ServerService, email, args[1])
		if err != nil {
			return err
		}
		if token != "" {
			fmt.Println(token)
		}
		return nil
	case "sign-in-link":
		application, err := app.New(cfg)
		if err != nil {
			return err
		}
		defer application.Close()
		link, err := application.AuthService.SignInLink(email)
		if err != nil {
			return err
		}
		fmt.Println(link)
		return nil
	}
	return fmt.Errorf("unknown command %q: bootstrap <server name> or sign-in-link", args[0])
}
