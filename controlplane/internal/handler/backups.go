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
	err := h.apps.Restore(r.Context(), actor(r), r.PathValue("app"), database, r.FormValue("snapshot"), r.FormValue("bucket"), at)
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

// SetAppBucket moves the app's uploads and new backups to another bucket.
func (h *AppsHandler) SetAppBucket(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("bucket")
	if name == "-default" {
		name = ""
	}
	app, err := h.apps.SetAppBucket(actor(r), r.PathValue("app"), name)
	if err != nil {
		failed(w, r, "Bucket not changed", err, nil)
		return
	}
	h.renderBackups(w, r)
	bucket := "the default bucket"
	if app.BucketName != "" {
		bucket = app.BucketName
	}
	toastDone(w, r, "Uploads and new backups go to "+bucket, "Older backups stay where they are; restores of them read from there.")
}

// Bucket is the buckets page.
func (h *AppsHandler) Bucket(w http.ResponseWriter, r *http.Request) {
	h.renderBucket(w, r)
}

// bucketFields are the fields of a bucket form, by API field name, with
// prefix the start of their IDs.
func bucketFields(prefix string) map[string]string {
	return map[string]string{"name": prefix + "name", "endpoint": prefix + "endpoint", "bucket": prefix + "bucket", "secret_key": prefix + "secret_key"}
}

func bucketInput(r *http.Request) service.BucketInput {
	return service.BucketInput{
		Name:      r.FormValue("name"),
		Endpoint:  r.FormValue("endpoint"),
		Region:    r.FormValue("region"),
		Bucket:    r.FormValue("bucket"),
		Path:      r.FormValue("path"),
		AccessKey: r.FormValue("access_key"),
		SecretKey: r.FormValue("secret_key"),
	}
}

// AddBucket checks and stores a new bucket.
func (h *AppsHandler) AddBucket(w http.ResponseWriter, r *http.Request) {
	bucket, err := h.apps.AddBucket(r.Context(), actor(r), bucketInput(r))
	if h.bucketFailed(w, r, "Bucket not added", "bucket-new-", err) {
		return
	}
	h.renderBucket(w, r)
	if bucket.Default {
		toastDone(w, r, "Bucket added", "Backups and ops deploy of every app use it now.")
		return
	}
	toastDone(w, r, "Bucket added", "Pick it for an app on the app's Backups tab.")
}

// EditBucket changes a bucket.
func (h *AppsHandler) EditBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	_, err := h.apps.EditBucket(r.Context(), actor(r), name, bucketInput(r))
	if h.bucketFailed(w, r, "Bucket not saved", "bucket-"+name+"-", err) {
		return
	}
	h.renderBucket(w, r)
	toastDone(w, r, "Bucket saved", "Its apps upload and back up there now.")
}

// DefaultBucket makes a bucket the workspace's default.
func (h *AppsHandler) DefaultBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	_, err := h.apps.MakeDefaultBucket(r.Context(), actor(r), name)
	if err != nil {
		failed(w, r, "Default not changed", err, nil)
		return
	}
	h.renderBucket(w, r)
	toastDone(w, r, name+" is the default bucket", "Apps without a bucket of their own use it now; their older backups stay in the old default.")
}

// RemoveBucket forgets a bucket nothing uses.
func (h *AppsHandler) RemoveBucket(w http.ResponseWriter, r *http.Request) {
	err := h.apps.RemoveBucket(actor(r), r.PathValue("name"))
	if err != nil {
		failed(w, r, "Bucket not removed", err, nil)
		return
	}
	h.renderBucket(w, r)
	toastDone(w, r, "Bucket removed", "What is in it stays there.")
}

// bucketFailed answers a bucket form that failed and reports whether it
// did. A check that could not write points at the secret key.
func (h *AppsHandler) bucketFailed(w http.ResponseWriter, r *http.Request, title, prefix string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, service.ErrBackupAccess):
		fieldRefused(w, r, title, prefix+"secret_key", errorMessage(err))
	default:
		failed(w, r, title, err, bucketFields(prefix))
	}
	return true
}

func (h *AppsHandler) renderBucket(w http.ResponseWriter, r *http.Request) {
	buckets, err := h.apps.Buckets(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		renderError(w, "load buckets failed", err)
		return
	}
	render(w, r, pages.Bucket(pages.BucketView{Buckets: buckets}))
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
	view.Storage, err = h.apps.AppBucket(app)
	if err != nil && !errors.Is(err, service.ErrNoBackups) {
		renderError(w, "load backup storage failed", err)
		return
	}
	if view.Storage != nil {
		view.Buckets, err = h.apps.Buckets(workspace.ID)
		if err != nil {
			renderError(w, "load buckets failed", err)
			return
		}
		view.Older, err = h.apps.OlderBuckets(app)
		if err != nil {
			renderError(w, "load older buckets failed", err)
			return
		}
		_, view.Backups, err = h.apps.Backups(r.Context(), workspace.ID, app.ID)
		if err != nil {
			view.Error = errorMessage(err)
		}
	}
	render(w, r, pages.AppBackups(view))
}
