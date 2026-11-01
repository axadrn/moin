package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/api"
	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// GitHubInstall sends the user to GitHub to install the ops GitHub App.
// next is where to come back to.
func (h *AppsHandler) GitHubInstall(w http.ResponseWriter, r *http.Request) {
	link, err := h.apps.GitHubInstallURL(actor(r))
	if err != nil {
		renderError(w, "github install link failed", err)
		return
	}
	next := r.URL.Query().Get("next")
	if strings.HasPrefix(next, "/app/") {
		http.SetCookie(w, &http.Cookie{Name: "github_next", Value: next, Path: "/app/github", MaxAge: 3600, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
	}
	http.Redirect(w, r, link, http.StatusSeeOther)
}

// GitHubSetup is where GitHub sends the user after the install. It is one
// URL for every workspace; the state names the workspace.
func (h *AppsHandler) GitHubSetup(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	next := "/app"
	cookie, err := r.Cookie("github_next")
	if err == nil && strings.HasPrefix(cookie.Value, "/app/") {
		next = cookie.Value
	}
	installationID, err := strconv.ParseInt(r.URL.Query().Get("installation_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	_, err = h.apps.ConnectGitHub(r.Context(), service.Actor{UserID: user.ID, Via: "web"}, installationID, r.URL.Query().Get("state"))
	if err != nil {
		slog.Warn("github setup failed", "user_id", user.ID, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, pages.ErrorPage(http.StatusBadRequest, "GitHub is not connected", errorMessage(err)))
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// GitHubWebhook takes the events of the ops GitHub App: pushes deploy the
// apps that follow the branch, an uninstall unlinks its apps.
func (h *HooksHandler) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		api.Error(w, http.StatusBadRequest, "invalid_body", "Could not read the body.")
		return
	}
	if !h.apps.VerifyGitHubWebhook(body, r.Header.Get("X-Hub-Signature-256")) {
		api.Error(w, http.StatusUnauthorized, "invalid_signature", "The signature does not match.")
		return
	}
	var event struct {
		Action       string `json:"action"`
		Ref          string `json:"ref"`
		After        string `json:"after"`
		Deleted      bool   `json:"deleted"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	err = json.Unmarshal(body, &event)
	if err != nil {
		api.Error(w, http.StatusBadRequest, "invalid_body", "The body is not JSON.")
		return
	}
	switch r.Header.Get("X-GitHub-Event") {
	case "push":
		branch, ok := strings.CutPrefix(event.Ref, "refs/heads/")
		if ok && !event.Deleted && event.After != "" {
			// GitHub waits at most ten seconds; the deploy reports through
			// the commit status.
			go h.apps.GitHubPush(context.WithoutCancel(r.Context()), event.Installation.ID, event.Repository.FullName, branch, event.After)
		}
	case "installation":
		if event.Action == "deleted" {
			err = h.apps.GitHubUninstalled(event.Installation.ID)
		}
	}
	if err != nil {
		api.ServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// Repo is the repository panel of an app.
func (h *AppsHandler) Repo(w http.ResponseWriter, r *http.Request) {
	h.renderRepo(w, r, "", "")
}

// LinkRepo makes the app follow the chosen repo and branch.
func (h *AppsHandler) LinkRepo(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.LinkRepo(r.Context(), actor(r), r.PathValue("app"), r.FormValue("repo"), r.FormValue("branch"), r.FormValue("auto_deploy") == "1")
	if err != nil {
		h.renderRepo(w, r, "", errorMessage(err))
		return
	}
	h.renderRepo(w, r, "Linked.", "")
}

// UnlinkRepo stops the app from following its repo.
func (h *AppsHandler) UnlinkRepo(w http.ResponseWriter, r *http.Request) {
	err := h.apps.UnlinkRepo(actor(r), r.PathValue("app"))
	if err != nil {
		h.renderRepo(w, r, "", errorMessage(err))
		return
	}
	h.renderRepo(w, r, "", "")
}

// DeployRef deploys a branch, tag or commit from GitHub; the page follows
// the deploy on its own.
func (h *AppsHandler) DeployRef(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.DeployRef(r.Context(), actor(r), r.PathValue("app"), r.FormValue("ref"))
	if err != nil {
		h.renderRepo(w, r, "", errorMessage(err))
		return
	}
	h.renderRepo(w, r, "Deploying "+shortSHA(deploy.CommitSHA)+" as v"+strconv.Itoa(deploy.Number)+".", "")
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (h *AppsHandler) renderRepo(w http.ResponseWriter, r *http.Request, notice, failure string) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.apps.Get(workspace.ID, r.PathValue("app"))
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	view := pages.RepoView{App: *app, Notice: notice, Error: failure}
	_, err = h.apps.GitHubInstallURL(actor(r))
	view.Enabled = err == nil
	installations, err := h.apps.GitHubInstallations(workspace.ID)
	if err != nil {
		renderError(w, "load github installations failed", err)
		return
	}
	view.Connected = len(installations) > 0
	if view.Enabled && view.Connected && app.Repo == "" {
		view.Repos, err = h.apps.GitHubRepos(r.Context(), workspace.ID)
		if err != nil && view.Error == "" {
			view.Error = errorMessage(err)
		}
	}
	render(w, r, pages.AppRepo(view))
}

// ServerSecurity is the security page of a server.
func (h *AppsHandler) ServerSecurity(w http.ResponseWriter, r *http.Request) {
	h.renderSecurity(w, r, "", "")
}

// SetServerSecurity applies the switches.
func (h *AppsHandler) SetServerSecurity(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.SetServerSecurity(r.Context(), actor(r), r.PathValue("id"), service.SecurityInput{
		Tunnel:       r.FormValue("tunnel") == "1",
		SSHTailscale: r.FormValue("ssh_tailscale") == "1",
		Firewall:     r.FormValue("firewall") == "1",
	})
	if err != nil {
		h.renderSecurity(w, r, "", errorMessage(err))
		return
	}
	h.renderSecurity(w, r, "Applied.", "")
}

// SetIntegrations stores the keys; an empty field keeps its key.
func (h *AppsHandler) SetIntegrations(w http.ResponseWriter, r *http.Request) {
	var cloudflare, tailscale *string
	cloudflareToken, tailscaleKey := r.FormValue("cloudflare_token"), r.FormValue("tailscale_key")
	if cloudflareToken != "" {
		cloudflare = &cloudflareToken
	}
	if tailscaleKey != "" {
		tailscale = &tailscaleKey
	}
	err := h.apps.SetIntegrations(r.Context(), actor(r), cloudflare, tailscale)
	r.SetPathValue("id", r.FormValue("server"))
	if err != nil {
		h.renderSecurity(w, r, "", errorMessage(err))
		return
	}
	h.renderSecurity(w, r, "Keys saved.", "")
}

func (h *AppsHandler) renderSecurity(w http.ResponseWriter, r *http.Request, notice, failure string) {
	workspace := ctxkeys.Workspace(r.Context())
	server, err := h.servers.Get(workspace.ID, r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	integrations, err := h.apps.Integrations(workspace.ID)
	if err != nil {
		renderError(w, "load integrations failed", err)
		return
	}
	render(w, r, pages.ServerSecurity(pages.SecurityView{Server: *server, Integrations: integrations, Notice: notice, Error: failure}))
}
