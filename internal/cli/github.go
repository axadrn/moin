package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/style"
)

// github shows where the ops GitHub App is installed and its repos.
func github(ctx context.Context, args []string, stdout io.Writer) error {
	api, err := apiClient()
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "repos" {
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
		table(stdout, []string{"REPO", "DEFAULT BRANCH", "VISIBILITY"}, rows)
		return nil
	}
	answer, err := api.GitHub(ctx)
	if err != nil {
		return err
	}
	if !answer.Enabled {
		return errors.New("this control plane has no GitHub App")
	}
	for _, installation := range answer.Installations {
		done(stdout, "Installed on %s", installation.Account)
	}
	if len(answer.Installations) == 0 {
		fmt.Fprintln(stdout, "GitHub is not connected yet.")
	}
	fmt.Fprintln(stdout, "Install the ops GitHub App on an account or organization:")
	fmt.Fprintln(stdout, "  "+style.Bold.Render(answer.InstallURL))
	fmt.Fprintln(stdout, style.Muted.Render("The link is yours and works for an hour."))
	return nil
}

// link makes an app follow a GitHub repo and branch.
func link(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("link", flag.ContinueOnError)
	appFlag := flags.String("app", "", "app name")
	branch := flags.String("branch", "", "branch to follow; default the repo's default branch")
	auto := flags.Bool("auto", true, "deploy every push to the branch")
	if err := parseAnywhere(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("name the repo: %s link owner/name [--branch main] [--auto=false]", build.Name)
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	app, err := api.LinkRepo(ctx, appName(*appFlag, "."), flags.Arg(0), *branch, *auto)
	if err != nil {
		return err
	}
	done(stdout, "%s follows %s@%s", app.Name, app.Repo, app.Branch)
	if app.AutoDeploy {
		fmt.Fprintln(stdout, style.Muted.Render("  every push to "+app.Branch+" deploys; deploy now with ")+style.Bold.Render(build.Name+" deploy --ref "+app.Branch))
	}
	return nil
}

// unlink stops an app from following its repo.
func unlink(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("unlink", flag.ContinueOnError)
	appFlag := flags.String("app", "", "app name")
	if err := parseAnywhere(flags, args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(*appFlag, ".")
	err = api.UnlinkRepo(ctx, name)
	if err != nil {
		return err
	}
	done(stdout, "%s follows no repo now.", name)
	return nil
}
