package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// VersionHandler answers the version of the hub, the newest
// release and the update, and the workspace's automatic daemon updates.
type VersionHandler struct {
	updates *service.UpdateService
	servers *service.ServerService
}

func NewVersionHandler(updates *service.UpdateService, servers *service.ServerService) *VersionHandler {
	return &VersionHandler{updates: updates, servers: servers}
}

// VersionInfo is the answer of GET /api/v1/version.
type VersionInfo struct {
	// Version is the hub's version, the one daemons update to.
	Version string `json:"version"`
	// SelfHosted is false on the hosted hub, which updates with
	// every release; the fields below stay empty there.
	SelfHosted      bool       `json:"self_hosted"`
	Latest          string     `json:"latest"`
	UpdateAvailable bool       `json:"update_available"`
	PublishedAt     *time.Time `json:"published_at"`
	NotesURL        string     `json:"notes_url"`
	Summary         string     `json:"summary"`
	Notes           string     `json:"notes"`
	CheckedAt       *time.Time `json:"checked_at"`
	// CanUpdate is true for an admin of a self-hosted hub while
	// an update is out and the updater runs.
	CanUpdate bool `json:"can_update"`
	// Updater says whether the updater runs next to the hub.
	Updater bool               `json:"updater"`
	Update  *model.UpdateState `json:"update"`
}

// NewVersion is the JSON shape of the version info.
func NewVersion(info model.VersionInfo) VersionInfo {
	return VersionInfo{
		Version: info.Version, SelfHosted: info.SelfHosted, Latest: info.Latest, UpdateAvailable: info.UpdateAvailable,
		PublishedAt: info.PublishedAt, NotesURL: info.NotesURL, Summary: info.Summary, Notes: info.Notes,
		CheckedAt: info.CheckedAt, CanUpdate: info.CanUpdate, Updater: info.Updater, Update: info.Update,
	}
}

// Show answers the version and the newest release.
func (h *VersionHandler) Show(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, NewVersion(h.updates.Info(ctxkeys.User(r.Context()))))
}

// Update asks the updater to install the newest release.
func (h *VersionHandler) Update(w http.ResponseWriter, r *http.Request) {
	info, err := h.updates.Start(actor(r), ctxkeys.User(r.Context()))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, NewVersion(info))
}

// DaemonUpdates is the answer of GET and PUT /api/v1/daemon-updates.
type DaemonUpdates struct {
	// Auto updates every daemon older than the hub by itself.
	Auto bool `json:"auto"`
}

// DaemonUpdates answers whether the daemons update by themselves.
func (h *VersionHandler) DaemonUpdates(w http.ResponseWriter, r *http.Request) {
	on, err := h.servers.AutoUpdateDaemons(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, DaemonUpdates{Auto: on})
}

// SetDaemonUpdates turns automatic daemon updates on or off.
func (h *VersionHandler) SetDaemonUpdates(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Auto *bool `json:"auto"`
	}
	if !Decode(w, r, &body) {
		return
	}
	if body.Auto == nil {
		ValidationError(w, map[string]string{"auto": "Say true or false."})
		return
	}
	if err := h.servers.SetAutoUpdateDaemons(actor(r), *body.Auto); err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, DaemonUpdates{Auto: *body.Auto})
}
