package api

import (
	"errors"
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
)

// GitHub is the answer of GET /api/v1/github: where the ops GitHub App is
// installed, and the link that installs it on another account.
type GitHub struct {
	Enabled       bool                 `json:"enabled"`
	InstallURL    string               `json:"install_url,omitempty"`
	Installations []GitHubInstallation `json:"installations"`
}

// GitHubInstallation is the app on one account or organization.
type GitHubInstallation struct {
	ID      int64  `json:"id"`
	Account string `json:"account"`
}

// GitHubRepos is the answer of GET /api/v1/github/repos.
type GitHubRepos struct {
	Items []GitHubRepo `json:"items"`
}

// GitHubRepo is a repo ops may read.
type GitHubRepo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Account       string `json:"account"`
}

// RepoRequest is the body of PUT /api/v1/apps/{app}/repo.
type RepoRequest struct {
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	AutoDeploy bool   `json:"auto_deploy"`
}

// GetGitHub returns the installations and the install link.
func (h *AppsHandler) GetGitHub(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	answer := GitHub{Installations: []GitHubInstallation{}}
	link, err := h.apps.GitHubInstallURL(actor(r))
	if err != nil && !errors.Is(err, service.ErrGitHubOff) {
		ServiceError(w, err)
		return
	}
	answer.Enabled, answer.InstallURL = err == nil, link
	installations, err := h.apps.GitHubInstallations(workspace.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	for _, installation := range installations {
		answer.Installations = append(answer.Installations, GitHubInstallation{ID: installation.InstallationID, Account: installation.Account})
	}
	JSON(w, http.StatusOK, answer)
}

// GetGitHubRepos lists the repos ops may read.
func (h *AppsHandler) GetGitHubRepos(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	repos, err := h.apps.GitHubRepos(r.Context(), workspace.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	answer := GitHubRepos{Items: []GitHubRepo{}}
	for _, repo := range repos {
		answer.Items = append(answer.Items, GitHubRepo{FullName: repo.FullName, DefaultBranch: repo.DefaultBranch, Private: repo.Private, Account: repo.Account})
	}
	JSON(w, http.StatusOK, answer)
}

// SetRepo makes the app follow a repo and branch.
func (h *AppsHandler) SetRepo(w http.ResponseWriter, r *http.Request) {
	var request RepoRequest
	if !Decode(w, r, &request) {
		return
	}
	app, err := h.apps.LinkRepo(r.Context(), actor(r), r.PathValue("app"), request.Repo, request.Branch, request.AutoDeploy)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewApp(*app))
}

// DeleteRepo stops the app from following its repo.
func (h *AppsHandler) DeleteRepo(w http.ResponseWriter, r *http.Request) {
	err := h.apps.UnlinkRepo(actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}
