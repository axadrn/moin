package client

import (
	"context"
	"net/http"
)

// GitHub is where the ops GitHub App is installed.
type GitHub struct {
	Enabled       bool   `json:"enabled"`
	InstallURL    string `json:"install_url"`
	Installations []struct {
		ID      int64  `json:"id"`
		Account string `json:"account"`
	} `json:"installations"`
}

// GitHubRepo is a repo ops may read.
type GitHubRepo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Account       string `json:"account"`
}

// GitHub returns the installations and the install link.
func (c *Client) GitHub(ctx context.Context) (GitHub, error) {
	var answer GitHub
	err := c.do(ctx, http.MethodGet, "/api/v1/github", nil, &answer)
	return answer, err
}

// GitHubRepos lists the repos ops may read.
func (c *Client) GitHubRepos(ctx context.Context) ([]GitHubRepo, error) {
	var answer struct {
		Items []GitHubRepo `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/github/repos", nil, &answer)
	return answer.Items, err
}

// LinkRepo makes an app follow a repo and branch.
func (c *Client) LinkRepo(ctx context.Context, app, repo, branch string, autoDeploy bool) (App, error) {
	var answer App
	err := c.do(ctx, http.MethodPut, appPath(app, "repo"), map[string]any{"repo": repo, "branch": branch, "auto_deploy": autoDeploy}, &answer)
	return answer, err
}

// UnlinkRepo stops an app from following its repo.
func (c *Client) UnlinkRepo(ctx context.Context, app string) error {
	return c.do(ctx, http.MethodDelete, appPath(app, "repo"), nil, nil)
}
