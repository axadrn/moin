package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
	"github.com/axadrn/ops/protocol"
)

// github shows where the ops GitHub App is installed and its repos.
func github(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("github", "github, github repos, or github branches owner/name")
	if err := flags.parse(args); err != nil {
		return err
	}
	wrong := flags.NArg() > 2 || (flags.NArg() >= 1 && flags.Arg(0) != "repos" && flags.Arg(0) != "branches") ||
		(flags.Arg(0) == "repos" && flags.NArg() != 1) || (flags.Arg(0) == "branches" && flags.NArg() != 2)
	if wrong {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if flags.Arg(0) == "branches" {
		branches, err := api.GitHubBranches(ctx, flags.Arg(1))
		if err != nil {
			return err
		}
		for index, branch := range branches {
			if index == 0 {
				branch += style.Muted.Render("  default")
			}
			fmt.Fprintln(stdout, branch)
		}
		hint(stdout, "Deploy one with", build.Name+" link "+flags.Arg(1)+" --branch <branch>, then "+build.Name+" deploy --ref <branch>")
		return nil
	}
	answer, err := api.GitHub(ctx)
	if err != nil {
		return err
	}
	if flags.Arg(0) == "repos" {
		repos, err := api.GitHubRepos(ctx)
		if err != nil {
			return err
		}
		rows := [][]string{}
		for _, repo := range repos {
			visibility := "public"
			if repo.Private {
				visibility = "private"
			}
			rows = append(rows, []string{repo.FullName, repo.DefaultBranch, visibility})
		}
		if len(rows) == 0 {
			fmt.Fprintln(stdout, build.Name+" may read no repo yet.")
		} else {
			table(stdout, []string{"REPO", "DEFAULT BRANCH", "VISIBILITY"}, rows)
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, style.Muted.Render("Don't see your repo? Give "+build.Name+" access to it on GitHub:"))
		for _, installation := range answer.Installations {
			fmt.Fprintln(stdout, "  "+installation.Account+"  "+style.Bold.Render(installation.SettingsURL))
		}
		if answer.InstallURL != "" {
			fmt.Fprintln(stdout, "  "+style.Muted.Render("another account or organization")+"  "+style.Bold.Render(answer.InstallURL))
		}
		return nil
	}
	if !answer.Enabled {
		return errors.New("this control plane has no GitHub App")
	}
	for _, installation := range answer.Installations {
		done(stdout, "Installed on %s", installation.Account)
	}
	if len(answer.Installations) == 0 {
		fmt.Fprintln(stdout, "GitHub is not connected yet. Connect it, also when the app is already installed:")
		fmt.Fprintln(stdout, "  "+style.Bold.Render(cmp.Or(answer.ConnectURL, answer.InstallURL)))
	}
	fmt.Fprintln(stdout, "Install the "+build.Name+" GitHub App on another account or organization:")
	fmt.Fprintln(stdout, "  "+style.Bold.Render(answer.InstallURL))
	fmt.Fprintln(stdout, style.Muted.Render("Each link is yours and works for an hour."))
	return nil
}

// link makes an app follow a GitHub repo and branch.
func link(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("link", "link owner/name [--branch main] [--auto=false] [app]").withApp()
	branch := flags.String("branch", "", "branch to follow; default the repo's default branch")
	auto := flags.Bool("auto", true, "deploy every push to the branch")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return flags.wrong()
	}
	if *branch != "" && !protocol.ValidRefName(*branch) {
		return fmt.Errorf("%q is not a valid branch name", *branch)
	}
	name, err := flags.app(flags.Arg(1))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	app, err := api.LinkRepo(ctx, name, flags.Arg(0), *branch, *auto)
	var apiErr *client.Error
	if errors.As(err, &apiErr) && apiErr.Code == "validation_failed" && strings.Contains(apiErr.Message, "cannot read this repo") {
		return fmt.Errorf("%s cannot read %s. Install the %s GitHub App on its account and give it the repo: `%s github` shows where it is installed and the link", build.Name, flags.Arg(0), build.Name, build.Name)
	}
	if err != nil {
		return err
	}
	done(stdout, "%s follows %s@%s", app.Name, app.Repo, app.Branch)
	lead := "Deploy it now with"
	if app.AutoDeploy {
		lead = "Every push to " + app.Branch + " deploys. Deploy it now with"
	}
	hint(stdout, lead, build.Name+" deploy --ref "+app.Branch+" --app "+app.Name)
	return nil
}

// unlink stops an app from following its repo.
func unlink(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("unlink", "unlink [app] [--yes]").withApp().withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	err = confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Stop %s from following its repo? Pushes no longer deploy it.", name))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	err = api.UnlinkRepo(ctx, name)
	if err != nil {
		return err
	}
	done(stdout, "%s follows no repo now.", name)
	return nil
}
