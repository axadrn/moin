package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
)

// StatusHandler answers whether everything is OK in a workspace.
type StatusHandler struct {
	status *service.StatusService
}

func NewStatusHandler(status *service.StatusService) *StatusHandler {
	return &StatusHandler{status: status}
}

// StatusIssue is one thing that needs you; Path is under the workspace in
// the web app.
type StatusIssue struct {
	Kind   string     `json:"kind"`
	Target string     `json:"target"`
	Detail string     `json:"detail"`
	Path   string     `json:"path"`
	Since  *time.Time `json:"since"`
}

// StatusUpdate is a catalog app with a newer release.
type StatusUpdate struct {
	App     string `json:"app"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Latest  string `json:"latest"`
}

// StatusCounts is what runs well.
type StatusCounts struct {
	Apps           int `json:"apps"`
	AppsLive       int `json:"apps_live"`
	Servers        int `json:"servers"`
	ServersOnline  int `json:"servers_online"`
	Databases      int `json:"databases"`
	BackupsChecked int `json:"backups_checked"`
}

// StatusResponse is the workspace at a glance: ok is false when an issue
// needs you; updates never make it false.
type StatusResponse struct {
	OK      bool           `json:"ok"`
	Issues  []StatusIssue  `json:"issues"`
	Updates []StatusUpdate `json:"updates"`
	Counts  StatusCounts   `json:"counts"`
}

// NewStatus is the JSON shape of a workspace status.
func NewStatus(status service.WorkspaceStatus) StatusResponse {
	response := StatusResponse{
		OK:      status.OK(),
		Issues:  make([]StatusIssue, 0, len(status.Issues)),
		Updates: make([]StatusUpdate, 0, len(status.Updates)),
		Counts: StatusCounts{
			Apps: status.Apps, AppsLive: status.AppsLive,
			Servers: status.Servers, ServersOnline: status.ServersOnline,
			Databases: status.Databases, BackupsChecked: status.BackupsChecked,
		},
	}
	for _, issue := range status.Issues {
		response.Issues = append(response.Issues, StatusIssue{Kind: issue.Kind, Target: issue.Target, Detail: issue.Detail, Path: issue.Path, Since: issue.Since})
	}
	for _, update := range status.Updates {
		response.Updates = append(response.Updates, StatusUpdate{App: update.App, Name: update.Name, Version: update.Version, Latest: update.Latest})
	}
	return response
}

// Get answers whether everything is OK.
func (h *StatusHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	status, err := h.status.Status(workspace.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewStatus(status))
}
