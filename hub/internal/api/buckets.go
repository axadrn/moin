package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/internal/link"
)

// Bucket is one bucket of the workspace, without its secret key, which
// only the servers keep: Servers are the ones that keep it, MissingOn the
// others. Folder is where this workspace's files go in it: path, then the
// workspace ID. Apps back up there now; OlderBackups are the apps that
// moved away and keep their older backups there.
type Bucket struct {
	ID           string    `json:"id"`
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
	SecretServers
}

// BucketList is the answer of GET /api/v1/buckets.
type BucketList struct {
	Items []Bucket `json:"items"`
}

// BucketRequest is the body of POST /api/v1/buckets and PUT
// /api/v1/buckets/{name}. Server is the server that checks the bucket and
// keeps its secret key, empty for the workspace's only one. On a change an
// empty secret_key keeps the key the server has and an empty name keeps
// the name; default true makes it the workspace's default bucket.
type BucketRequest struct {
	Server    string `json:"server"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Path      string `json:"path"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Default   bool   `json:"default"`
}

// BucketKeyRequest is the body of PUT /api/v1/buckets/{name}/key: the
// secret key for one more server.
type BucketKeyRequest struct {
	Server    string `json:"server"`
	SecretKey string `json:"secret_key"`
}

// AppBucketRequest is the body of PUT /api/v1/apps/{app}/bucket. An empty
// bucket follows the workspace's default bucket.
type AppBucketRequest struct {
	Bucket string `json:"bucket"`
}

// NewBucket is the JSON shape of a bucket.
func NewBucket(bucket service.Bucket) Bucket {
	return Bucket{
		ID:            link.ID(bucket.ID),
		Name:          bucket.Name,
		Default:       bucket.Default,
		Endpoint:      bucket.Endpoint,
		Region:        bucket.Region,
		Bucket:        bucket.Bucket,
		Path:          bucket.Path,
		Folder:        bucket.Folder,
		AccessKey:     bucket.AccessKey,
		Apps:          bucket.Apps,
		OlderBackups:  bucket.Older,
		CreatedAt:     bucket.CreatedAt.UTC(),
		SecretServers: NewSecretServers(bucket.Servers),
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

// SetBucketKey hands the secret key of a bucket to one more server, which
// checks the bucket with it and keeps it.
func (h *AppsHandler) SetBucketKey(w http.ResponseWriter, r *http.Request) {
	var request BucketKeyRequest
	if !Decode(w, r, &request) {
		return
	}
	bucket, err := h.apps.SetBucketKey(r.Context(), actor(r), r.PathValue("name"), request.Server, request.SecretKey)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewBucket(*bucket))
}

// DeleteBucket removes a bucket nothing uses; ?confirm= is its name.
func (h *AppsHandler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	err := h.apps.RemoveBucket(actor(r), r.PathValue("name"), r.URL.Query().Get("confirm"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}

// SetAppBucket moves an app's new backups to a bucket.
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
