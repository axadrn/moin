package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
)

// Bucket is one bucket of the workspace, without its secret key. Folder is
// where this workspace's files go in it: path, then the workspace ID. Apps
// upload and back up there now; OlderBackups are the apps that moved away
// and keep their older backups there.
type Bucket struct {
	Name         string    `json:"name"`
	Default      bool      `json:"default"`
	Endpoint     string    `json:"endpoint"`
	Region       string    `json:"region"`
	Bucket       string    `json:"bucket"`
	Path         string    `json:"path"`
	Folder       string    `json:"folder"`
	AccessKey    string    `json:"access_key"`
	Apps         []string  `json:"apps"`
	OlderBackups []string  `json:"older_backups"`
	CreatedAt    time.Time `json:"created_at"`
}

// BucketList is the answer of GET /api/v1/buckets.
type BucketList struct {
	Items []Bucket `json:"items"`
}

// BucketRequest is the body of POST /api/v1/buckets and PUT
// /api/v1/buckets/{name}. On a change an empty secret_key keeps the stored
// one and an empty name keeps the name; default true makes it the
// workspace's default bucket.
type BucketRequest struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Path      string `json:"path"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Default   bool   `json:"default"`
}

// AppBucketRequest is the body of PUT /api/v1/apps/{app}/bucket. An empty
// bucket follows the workspace's default bucket.
type AppBucketRequest struct {
	Bucket string `json:"bucket"`
}

// NewBucket is the JSON shape of a bucket.
func NewBucket(bucket service.Bucket) Bucket {
	return Bucket{
		Name:         bucket.Name,
		Default:      bucket.Default,
		Endpoint:     bucket.Endpoint,
		Region:       bucket.Region,
		Bucket:       bucket.Bucket,
		Path:         bucket.Path,
		Folder:       bucket.Folder,
		AccessKey:    bucket.AccessKey,
		Apps:         bucket.Apps,
		OlderBackups: bucket.Older,
		CreatedAt:    bucket.CreatedAt.UTC(),
	}
}

// Buckets lists the workspace's buckets, the default first.
func (h *AppsHandler) Buckets(w http.ResponseWriter, r *http.Request) {
	buckets, err := h.apps.Buckets(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	list := BucketList{Items: make([]Bucket, 0, len(buckets))}
	for _, bucket := range buckets {
		list.Items = append(list.Items, NewBucket(bucket))
	}
	JSON(w, http.StatusOK, list)
}

// GetBucket returns one bucket.
func (h *AppsHandler) GetBucket(w http.ResponseWriter, r *http.Request) {
	bucket, err := h.apps.Bucket(ctxkeys.Workspace(r.Context()).ID, r.PathValue("name"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewBucket(*bucket))
}

// AddBucket checks and stores a new bucket.
func (h *AppsHandler) AddBucket(w http.ResponseWriter, r *http.Request) {
	var request BucketRequest
	if !Decode(w, r, &request) {
		return
	}
	bucket, err := h.apps.AddBucket(r.Context(), actor(r), service.BucketInput(request))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusCreated, NewBucket(*bucket))
}

// EditBucket changes a bucket.
func (h *AppsHandler) EditBucket(w http.ResponseWriter, r *http.Request) {
	var request BucketRequest
	if !Decode(w, r, &request) {
		return
	}
	bucket, err := h.apps.EditBucket(r.Context(), actor(r), r.PathValue("name"), service.BucketInput(request))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewBucket(*bucket))
}

// DeleteBucket removes a bucket nothing uses.
func (h *AppsHandler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	err := h.apps.RemoveBucket(actor(r), r.PathValue("name"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}

// SetAppBucket moves an app's uploads and new backups to a bucket.
func (h *AppsHandler) SetAppBucket(w http.ResponseWriter, r *http.Request) {
	var request AppBucketRequest
	if !Decode(w, r, &request) {
		return
	}
	app, err := h.apps.SetAppBucket(actor(r), r.PathValue("app"), request.Bucket)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewApp(*app))
}
