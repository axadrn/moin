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
	h.renderBackups(w, r)
}

// BackupNow takes a snapshot now.
func (h *AppsHandler) BackupNow(w http.ResponseWriter, r *http.Request) {
	_, _, err := h.apps.BackUp(r.Context(), actor(r), r.PathValue("app"))
	if err != nil {
		failed(w, r, "Snapshot not taken", err, nil)
		return
	}
	h.renderBackups(w, r)
	toastDone(w, r, "Snapshot taken", "It is the newest under Snapshots.")
}

// BackupRestore restores a database from a snapshot or a point in time.
func (h *AppsHandler) BackupRestore(w http.ResponseWriter, r *http.Request) {
	database := r.FormValue("database")
	// The point in time form sends a date from its calendar and a time.
	var at *time.Time
	if date := r.FormValue("date"); date != "" {
		raw := date + "T" + r.FormValue("time")
		parsed, err := time.Parse("2006-01-02T15:04:05", raw)
		if err != nil {
			parsed, err = time.Parse("2006-01-02T15:04", raw)
		}
		if err != nil {
			toastFailed(w, r, database+" not restored", "Choose a date and a time.")
			return
		}
		at = &parsed
	}
	err := h.apps.Restore(r.Context(), actor(r), r.PathValue("app"), database, r.FormValue("snapshot"), at)
	if err != nil {
		failed(w, r, database+" not restored", err, nil)
		return
	}
	h.renderBackups(w, r)
	toastDone(w, r, database+" restored", "The app runs again. The state before is a snapshot.")
}

// BackupSettings turns the snapshot before every change on or off. The
// switch says it; when saving fails, it flips back to what is stored.
func (h *AppsHandler) BackupSettings(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.SetSnapshotBeforeChanges(actor(r), r.PathValue("app"), r.FormValue("on") == "1")
	if err == nil {
		return
	}
	toastFailed(w, r, "Setting not saved", errorMessage(err))
	if app, err := h.apps.Get(ctxkeys.Workspace(r.Context()).ID, r.PathValue("app")); err == nil {
		renderOOB(w, r, pages.BackupSettingsForm(*app), "outerHTML:#backup-settings-form")
	}
}

// Bucket is the bucket page.
func (h *AppsHandler) Bucket(w http.ResponseWriter, r *http.Request) {
	h.renderBucket(w, r)
}

// bucketFields are the fields of the bucket form, by API field name.
var bucketFields = map[string]string{"endpoint": "bucket-endpoint", "bucket": "bucket-bucket"}

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
	if errors.Is(err, service.ErrBackupAccess) {
		fieldRefused(w, r, "Bucket not saved", "bucket-secret_key", errorMessage(err))
		return
	}
	if err != nil {
		failed(w, r, "Bucket not saved", err, bucketFields)
		return
	}
	h.renderBucket(w, r)
	toastDone(w, r, "Bucket saved", "Backups and ops deploy use it now, on every server.")
}

// RemoveBucket forgets the bucket; backups stop.
func (h *AppsHandler) RemoveBucket(w http.ResponseWriter, r *http.Request) {
	err := h.apps.RemoveBackupStorage(actor(r))
	if err != nil {
		failed(w, r, "Bucket not removed", err, nil)
		return
	}
	h.renderBucket(w, r)
	toastDone(w, r, "Bucket removed", "What is in it stays there.")
}

func (h *AppsHandler) renderBucket(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	storage, err := h.apps.BackupStorage(workspace.ID)
	if err != nil && !errors.Is(err, service.ErrNoBackups) {
		renderError(w, "load bucket failed", err)
		return
	}
	render(w, r, pages.Bucket(pages.BucketView{Storage: storage}))
}

// renderBackups renders the backups page. Error says why the server could
// not report its backups: a state of the page, not of a click.
func (h *AppsHandler) renderBackups(w http.ResponseWriter, r *http.Request) {
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
	view := pages.BackupsView{App: *app, Now: time.Now()}
	view.Storage, err = h.apps.BackupStorage(workspace.ID)
	if err != nil && !errors.Is(err, service.ErrNoBackups) {
		renderError(w, "load backup storage failed", err)
		return
	}
	if view.Storage != nil {
		_, view.Backups, err = h.apps.Backups(r.Context(), workspace.ID, app.ID)
		if err != nil {
			view.Error = errorMessage(err)
		}
	}
	render(w, r, pages.AppBackups(view))
}
