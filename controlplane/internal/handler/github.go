package handler

import (
	"errors"
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
	_, err = h.apps.ConnectGitHub(r.Context(), service.Actor{UserID: user.ID, Source: service.RequestSource(r.Context())}, installationID, r.URL.Query().Get("state"))
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
	h.renderRepo(w, r)
}

// LinkRepo makes the app follow the chosen repo and branch. The switch of
// a linked repo sends the same, with only deploy on push changed.
func (h *AppsHandler) LinkRepo(w http.ResponseWriter, r *http.Request) {
	before, err := h.apps.Get(ctxkeys.Workspace(r.Context()).ID, r.PathValue("app"))
	if err != nil {
		failed(w, r, "Repo not linked", err, nil)
		return
	}
	app, err := h.apps.LinkRepo(r.Context(), actor(r), r.PathValue("app"), r.FormValue("repo"), r.FormValue("branch"), r.FormValue("auto_deploy") == "1")
	switch {
	case err != nil && before.Repo != "":
		// The switch flips back to what is stored.
		h.renderRepo(w, r)
		toastError(w, r, "Deploy on push not changed", errorMessage(err))
	case errors.Is(err, service.ErrRepo):
		fieldRefused(w, r, "Repo not linked", "link-repo", errorMessage(err))
	case err != nil:
		failed(w, r, "Repo not linked", err, map[string]string{"repo": "link-repo", "ref": "link-branch"})
	case before.Repo != "":
		// The switch saved itself; its position says so.
		h.renderRepo(w, r)
	default:
		h.renderRepo(w, r)
		toastDone(w, r, app.Repo+" linked", "Deploy a branch, tag or commit below.")
	}
}

// UnlinkRepo stops the app from following its repo.
func (h *AppsHandler) UnlinkRepo(w http.ResponseWriter, r *http.Request) {
	err := h.apps.UnlinkRepo(actor(r), r.PathValue("app"))
	if err != nil {
		failed(w, r, "Repo not unlinked", err, nil)
		return
	}
	h.renderRepo(w, r)
	toastDone(w, r, "Repo unlinked", "ops no longer deploys its pushes. The app keeps running.")
}

// DeployRef deploys a branch, tag or commit from GitHub; the page follows
// the deploy on its own.
func (h *AppsHandler) DeployRef(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.DeployRef(r.Context(), actor(r), r.PathValue("app"), r.FormValue("ref"))
	if err != nil {
		failed(w, r, "Deploy not started", err, map[string]string{"ref": "deploy-ref"})
		return
	}
	h.renderRepo(w, r)
	toastDone(w, r, "Deploy v"+strconv.Itoa(deploy.Number)+" started", "It builds "+shortSHA(deploy.CommitSHA)+" from GitHub.")
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// renderRepo renders the repository panel. Error says why the repos could
// not be listed: a state of the panel, not of a click.
func (h *AppsHandler) renderRepo(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.apps.Get(workspace.ID, r.PathValue("app"))
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	view := pages.RepoView{App: *app}
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
		if err != nil {
			view.Error = errorMessage(err)
		}
	}
	render(w, r, pages.AppRepo(view))
}

// ServerSecurity is the security page of a server.
func (h *AppsHandler) ServerSecurity(w http.ResponseWriter, r *http.Request) {
	h.renderSecurity(w, r)
}

// SetServerSecurity applies the switches. The form also sends what the page
// showed (was_*): a switch nobody flipped keeps the server's setting as it is
// now, so a page opened before a change elsewhere does not undo it.
func (h *AppsHandler) SetServerSecurity(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	server, err := h.servers.Get(workspace.ID, r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = h.apps.SetServerSecurity(r.Context(), actor(r), server.ID, service.SecurityInput{
		Tunnel:       flipped(r, "tunnel", server.Tunnel),
		SSHTailscale: flipped(r, "ssh_tailscale", server.SSHTailscale),
		Firewall:     flipped(r, "firewall", server.Firewall),
	})
	if err != nil {
		failed(w, r, "Security not applied", err, nil)
		return
	}
	h.renderSecurity(w, r)
	toastDone(w, r, "Security applied", "The agent changes "+server.Name+" now.")
}

// flipped is the switch's new value when the form flipped it, else now.
func flipped(r *http.Request, name string, now bool) bool {
	on := r.FormValue(name) == "1"
	was := r.FormValue("was_" + name)
	if was != "" && on == (was == "1") {
		return now
	}
	return on
}

// SetIntegrations stores the keys; an empty field keeps its key. remove
// names a key to take away instead.
func (h *AppsHandler) SetIntegrations(w http.ResponseWriter, r *http.Request) {
	var cloudflare, tailscale *string
	cloudflareToken, tailscaleKey := r.FormValue("cloudflare_token"), r.FormValue("tailscale_key")
	done, notDone := "Keys saved", "Keys not saved"
	switch r.FormValue("remove") {
	case "cloudflare":
		cloudflareToken, tailscaleKey = "", ""
		done, notDone = "Cloudflare API token removed", "Cloudflare API token not removed"
		cloudflare = &cloudflareToken
	case "tailscale":
		cloudflareToken, tailscaleKey = "", ""
		done, notDone = "Tailscale auth key removed", "Tailscale auth key not removed"
		tailscale = &tailscaleKey
	}
	if cloudflareToken != "" {
		cloudflare = &cloudflareToken
	}
	if tailscaleKey != "" {
		tailscale = &tailscaleKey
	}
	err := h.apps.SetIntegrations(r.Context(), actor(r), cloudflare, tailscale)
	r.SetPathValue("id", r.FormValue("server"))
	if errors.Is(err, service.ErrCloudflareToken) {
		fieldRefused(w, r, notDone, "cloudflare-token", errorMessage(err))
		return
	}
	if err != nil {
		failed(w, r, notDone, err, map[string]string{"tailscale_key": "tailscale-key"})
		return
	}
	h.renderSecurity(w, r)
	toastDone(w, r, done, "")
}

// RenameServer gives the server a new name and answers its page with it.
func (h *AppsHandler) RenameServer(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	server, err := h.servers.Get(workspace.ID, r.PathValue("id"))
	if err != nil {
		failed(w, r, "Server not renamed", err, nil)
		return
	}
	renamed, err := h.servers.Rename(actor(r), server.ID, r.FormValue("name"))
	if err != nil {
		failed(w, r, "Server not renamed", err, map[string]string{"name": "server-rename"})
		return
	}
	h.renderSecurity(w, r)
	toastDone(w, r, server.Name+" renamed", "It is "+renamed.Name+" now. Its apps stay on it.")
}

func (h *AppsHandler) renderSecurity(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	server, err := h.servers.Get(workspace.ID, r.PathValue("id"))
	if err != nil && isAction(r) {
		// A server removed meanwhile sends the page to the list.
		w.Header().Set("HX-Redirect", ctxkeys.AppBasePath(r.Context())+"/servers")
		return
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	integrations, err := h.apps.Integrations(workspace.ID)
	if err != nil {
		renderError(w, "load integrations failed", err)
		return
	}
	render(w, r, pages.ServerSecurity(pages.SecurityView{Server: *server, Integrations: integrations}))
}
