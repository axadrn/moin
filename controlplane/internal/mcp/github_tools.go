package mcp

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/api"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerGitHubTools registers the tools of GitHub: the connection, repos
// and branches, linking a repo and deploying from it.
func registerGitHubTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	mcpsdk.AddTool(server, newTool("get_github", "GitHub connection",
		"Returns where the moin GitHub App is installed, connect_url, the link that connects GitHub (it also works when the app is already installed), and install_url, which installs it on another account; both happen on github.com. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getGitHub)))
	mcpsdk.AddTool(server, newTool("list_repos", "List GitHub repos",
		"Lists the repos the moin GitHub App may read, and where on github.com the user gives it access to another repo. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listRepos)))
	mcpsdk.AddTool(server, newTool("list_branches", "List branches",
		"Lists the branches of a repo the moin GitHub App may read, the default branch first. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listBranches)))
	mcpsdk.AddTool(server, newTool("link_repo", "Link a repo",
		"Makes an app follow a GitHub repo and branch; with auto_deploy every push to the branch deploys. A branch the repo does not have on GitHub is refused; list_branches lists them. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.linkRepo)))
	mcpsdk.AddTool(server, newTool("unlink_repo", "Unlink the repo",
		"Stops an app from following its GitHub repo. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.unlinkRepo)))
	mcpsdk.AddTool(server, newTool("deploy_ref", "Deploy from GitHub",
		"Deploys a branch, tag or commit of the app's linked GitHub repo, without a checkout; empty ref deploys the linked branch. Follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.deployRef)))
}

// RepoInput is the input of link_repo.
type RepoInput struct {
	App        string `json:"app" jsonschema:"the app name or ID"`
	Repo       string `json:"repo" jsonschema:"owner/name on GitHub"`
	Branch     string `json:"branch,omitempty" jsonschema:"the branch to follow; empty for the default branch"`
	AutoDeploy bool   `json:"auto_deploy,omitempty" jsonschema:"deploy every push to the branch"`
}

// RefInput is the input of deploy_ref.
type RefInput struct {
	App string `json:"app" jsonschema:"the app name or ID"`
	Ref string `json:"ref,omitempty" jsonschema:"a branch, tag or commit SHA; empty for the linked branch"`
}

// GitHub is the output of get_github.
type GitHub struct {
	api.GitHub
}

// Repos is the output of list_repos. Access says where on GitHub the user
// gives moin a repo it does not see yet.
type Repos struct {
	api.GitHubRepos
	Access string `json:"access"`
}

func (r Repos) Summary() string {
	names := make([]string, 0, len(r.Items))
	for _, repo := range r.Items {
		names = append(names, repo.FullName)
	}
	if len(names) == 0 {
		return "moin may read no repo yet. " + r.Access
	}
	return "moin may read " + strings.Join(names, ", ") + ". " + r.Access
}

// BranchesInput is the input of list_branches.
type BranchesInput struct {
	Repo string `json:"repo" jsonschema:"owner/name on GitHub"`
}

// Branches is the output of list_branches.
type Branches struct {
	api.GitHubBranches
}

func (t moinTools) getGitHub(_ context.Context, caller Caller, _ struct{}) (GitHub, error) {
	answer := api.GitHub{Installations: []api.GitHubInstallation{}}
	link, err := t.services.Apps.GitHubInstallURL(caller.actor())
	answer.Enabled, answer.InstallURL = err == nil, link
	answer.ConnectURL, _ = t.services.Apps.GitHubConnectURL(caller.actor())
	installations, err := t.services.Apps.GitHubInstallations(caller.Token.WorkspaceID)
	if err != nil {
		return GitHub{}, err
	}
	for _, installation := range installations {
		answer.Installations = append(answer.Installations, api.GitHubInstallation{ID: installation.InstallationID, Account: installation.Account, SettingsURL: cmp.Or(installation.SettingsURL, link)})
	}
	return GitHub{answer}, nil
}

func (t moinTools) listRepos(ctx context.Context, caller Caller, _ struct{}) (Repos, error) {
	repos, err := t.services.Apps.GitHubRepos(ctx, caller.Token.WorkspaceID)
	if err != nil {
		return Repos{}, err
	}
	answer := api.GitHubRepos{Items: []api.GitHubRepo{}}
	for _, repo := range repos {
		answer.Items = append(answer.Items, api.GitHubRepo{FullName: repo.FullName, DefaultBranch: repo.DefaultBranch, Private: repo.Private, Account: repo.Account})
	}
	github, err := t.getGitHub(ctx, caller, struct{}{})
	if err != nil {
		return Repos{}, err
	}
	links := []string{}
	for _, installation := range github.Installations {
		links = append(links, installation.Account+": "+installation.SettingsURL)
	}
	access := "Don't see a repo? The user gives moin access to it on GitHub"
	if len(links) > 0 {
		access += ", " + strings.Join(links, "; ")
	}
	if github.InstallURL != "" {
		access += ". Another account or organization: " + github.InstallURL
	}
	return Repos{answer, access + "."}, nil
}

func (t moinTools) listBranches(ctx context.Context, caller Caller, input BranchesInput) (Branches, error) {
	branches, err := t.services.Apps.GitHubBranches(ctx, caller.Token.WorkspaceID, input.Repo)
	if err != nil {
		return Branches{}, err
	}
	return Branches{api.GitHubBranches{Items: branches}}, nil
}

// LinkedApp is the output of link_repo.
type LinkedApp struct {
	api.App
}

func (a LinkedApp) Summary() string {
	next := "Deploy it with deploy_ref."
	if a.AutoDeploy {
		next = "Every push to " + a.Branch + " deploys it; deploy_ref deploys it now."
	}
	return fmt.Sprintf("%s follows %s on %s. %s", a.Name, a.Repo, a.Branch, next)
}

func (t moinTools) linkRepo(ctx context.Context, caller Caller, input RepoInput) (LinkedApp, error) {
	app, err := t.services.Apps.LinkRepo(ctx, caller.actor(), input.App, input.Repo, input.Branch, input.AutoDeploy)
	if err != nil {
		return LinkedApp{}, err
	}
	return LinkedApp{api.NewApp(*app)}, nil
}

func (t moinTools) unlinkRepo(_ context.Context, caller Caller, input AppInput) (Done, error) {
	app, err := t.services.Apps.Get(caller.Token.WorkspaceID, input.app())
	if err != nil {
		return Done{}, err
	}
	if app.Repo == "" {
		return Done{Message: input.app() + " follows no repo. Nothing changed."}, nil
	}
	err = t.services.Apps.UnlinkRepo(caller.actor(), input.app())
	if err != nil {
		return Done{}, err
	}
	return Done{Message: input.app() + " follows no repo now."}, nil
}

func (t moinTools) deployRef(ctx context.Context, caller Caller, input RefInput) (Deploy, error) {
	deploy, err := t.services.Apps.DeployRef(ctx, caller.actor(), input.App, input.Ref)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}
