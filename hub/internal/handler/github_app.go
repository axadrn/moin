package handler

import (
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// GitHubAppHandler creates the hub's GitHub App from a manifest.
type GitHubAppHandler struct {
	githubApp  *service.GitHubAppService
	workspaces *service.WorkspaceService
}

func NewGitHubAppHandler(githubApp *service.GitHubAppService, workspaces *service.WorkspaceService) *GitHubAppHandler {
	return &GitHubAppHandler{githubApp: githubApp, workspaces: workspaces}
}

// Created is where GitHub sends the admin back after creating the app,
// with its code and the state: the hub trades the code for the app and
// uses it at once. The activity of the admin's first workspace notes it.
func (h *GitHubAppHandler) Created(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	actor := service.Actor{UserID: user.ID, Source: service.RequestSource(r.Context())}
	if members, err := h.workspaces.ListForUser(user.ID); err == nil && len(members) > 0 {
		actor.WorkspaceID = members[0].ID
	}
	query := r.URL.Query()
	err := h.githubApp.Create(r.Context(), actor, user, query.Get("code"), query.Get("state"))
	if err != nil {
		status, detail, known := api.ErrorFor(err)
		if known {
			slog.Warn("github app not created", "user_id", user.ID, "error", err)
		} else {
			slog.Error("github app not created", "user_id", user.ID, "error", err)
		}
		w.WriteHeader(status)
		render(w, r, pages.ErrorPage(status, "GitHub App not created", detail.Message))
		return
	}
	slog.Info("github app created", "user_id", user.ID)
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}
