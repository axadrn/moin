package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// github shows where the ops GitHub App is installed and its repos.
func github(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("github", "github, or github repos")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 || (flags.NArg() == 1 && flags.Arg(0) != "repos") {
		return flags.wrong()
	}
	api, err := apiClient()
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
	flags := newFlags("link", "link owner/name [--branch main] [--auto=false] [app]").withApp()
	branch := flags.String("branch", "", "branch to follow; default the repo's default branch")
	auto := flags.Bool("auto", true, "deploy every push to the branch")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return flags.wrong()
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
	if app.AutoDeploy {
		fmt.Fprintln(stdout, style.Muted.Render("  every push to "+app.Branch+" deploys; deploy now with ")+style.Bold.Render(build.Name+" deploy --ref "+app.Branch))
	}
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
