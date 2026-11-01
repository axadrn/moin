package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// Backups is the backups page of an app.
func (h *AppsHandler) Backups(w http.ResponseWriter, r *http.Request) {
	h.renderBackups(w, r, "", "")
}

// BackupNow takes a snapshot now.
func (h *AppsHandler) BackupNow(w http.ResponseWriter, r *http.Request) {
	_, _, err := h.apps.BackUp(r.Context(), actor(r), r.PathValue("app"))
	if err != nil {
		h.renderBackups(w, r, "", errorMessage(err))
		return
	}
	h.renderBackups(w, r, "Snapshot taken.", "")
}

// BackupRestore restores a database from a snapshot or a point in time.
func (h *AppsHandler) BackupRestore(w http.ResponseWriter, r *http.Request) {
	// The point in time form sends a date from its calendar and a time.
	var at *time.Time
	if date := r.FormValue("date"); date != "" {
		raw := date + "T" + r.FormValue("time")
		parsed, err := time.Parse("2006-01-02T15:04:05", raw)
		if err != nil {
			parsed, err = time.Parse("2006-01-02T15:04", raw)
		}
		if err != nil {
			h.renderBackups(w, r, "", "Choose a date and time.")
			return
		}
		at = &parsed
	}
	err := h.apps.Restore(r.Context(), actor(r), r.PathValue("app"), r.FormValue("database"), r.FormValue("snapshot"), at)
	if err != nil {
		h.renderBackups(w, r, "", errorMessage(err))
		return
	}
	h.renderBackups(w, r, "Restored "+r.FormValue("database")+". The app runs again; the state before is a snapshot.", "")
}

// BackupSettings turns the snapshot before every change on or off.
func (h *AppsHandler) BackupSettings(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.SetSnapshotBeforeChanges(actor(r), r.PathValue("app"), r.FormValue("on") == "1")
	if err != nil {
		h.toast(w, r, "Setting not saved", err)
	}
}

// Bucket is the bucket page.
func (h *AppsHandler) Bucket(w http.ResponseWriter, r *http.Request) {
	h.renderBucket(w, r, "", "")
}

// SaveBucket checks and stores the bucket.
func (h *AppsHandler) SaveBucket(w http.ResponseWriter, r *http.Request) {
	err := h.apps.SetBackupStorage(r.Context(), actor(r), service.BackupStorageInput{
		Endpoint:  r.FormValue("endpoint"),
		Region:    r.FormValue("region"),
		Bucket:    r.FormValue("bucket"),
		Path:      r.FormValue("path"),
		AccessKey: r.FormValue("access_key"),
		SecretKey: r.FormValue("secret_key"),
	})
	if err != nil {
		h.renderBucket(w, r, "", errorMessage(err))
		return
	}
	h.renderBucket(w, r, "Saved. Backups and ops deploy use this bucket now, on every server.", "")
}

// RemoveBucket forgets the bucket; backups stop.
func (h *AppsHandler) RemoveBucket(w http.ResponseWriter, r *http.Request) {
	err := h.apps.RemoveBackupStorage(actor(r))
	if err != nil {
		h.renderBucket(w, r, "", errorMessage(err))
		return
	}
	h.renderBucket(w, r, "Removed. What is in the bucket stays.", "")
}

func (h *AppsHandler) renderBucket(w http.ResponseWriter, r *http.Request, notice, failure string) {
	workspace := ctxkeys.Workspace(r.Context())
	storage, err := h.apps.BackupStorage(workspace.ID)
	if err != nil && !errors.Is(err, service.ErrNoBackups) {
		renderError(w, "load bucket failed", err)
		return
	}
	render(w, r, pages.Bucket(pages.BucketView{Storage: storage, Notice: notice, Error: failure}))
}

func (h *AppsHandler) renderBackups(w http.ResponseWriter, r *http.Request, notice, failure string) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.apps.Get(workspace.ID, r.PathValue("app"))
	if errors.Is(err, repository.ErrAppNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	view := pages.BackupsView{App: *app, Notice: notice, Error: failure, Now: time.Now()}
	view.Storage, err = h.apps.BackupStorage(workspace.ID)
	if err != nil && !errors.Is(err, service.ErrNoBackups) {
		renderError(w, "load backup storage failed", err)
		return
	}
	if view.Storage != nil {
		_, view.Backups, err = h.apps.Backups(r.Context(), workspace.ID, app.ID)
		if err != nil && view.Error == "" {
			view.Error = errorMessage(err)
		}
	}
	render(w, r, pages.AppBackups(view))
}
