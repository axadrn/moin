package mcp

import (
	"context"
	"github.com/axadrn/moin/hub/internal/model"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/service"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerBucketTools registers the tools of the buckets backups go
// to.
func registerBucketTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	Add(server, newTool("list_buckets", "List buckets",
		"Lists the workspace's S3-compatible buckets, the default first, without their secret keys: where each is, folder (path, then the workspace ID), apps (the apps that back up there now), older_backups (apps that moved to another bucket and left what they had there), servers (the servers that keep its secret key) and missing_on (the servers that do not: their apps make no backups until set_bucket_key gives them the key).",
		readOnly("")), model.RightRead, member(services, tools.listBuckets))
	Add(server, newTool("add_bucket", "Add a bucket",
		"Adds an S3-compatible bucket under a name (lowercase letters, digits, dashes). The secret key goes to server (empty for the workspace's only server), which writes a test file and keeps the key; moin keeps everything but the secret key, which it never shows again. Other servers get it with set_bucket_key. The first bucket is the default; default true makes this one the default, so every app without a bucket of its own backs up there. path is an optional folder inside the bucket, only for a bucket that holds other files too.",
		change(false, false)), model.RightServers, changes(services, tools.addBucket))
	Add(server, newTool("edit_bucket", "Change a bucket",
		"Changes the bucket named name: send every field as it should be (get them from list_buckets); rename renames it, default true makes it the default. A new place is checked by server (empty for the workspace's only server) with the secret_key sent, or the key it keeps when empty. A new access key or secret key leaves the other servers without one: give it to them with set_bucket_key. A new place starts with a full copy; the old place keeps what it has.",
		change(false, true)), model.RightServers, changes(services, tools.editBucket))
	Add(server, newTool("set_bucket_key", "Give a server a bucket's key",
		"Gives one more server the secret key of a bucket, such as a server in missing_on of list_buckets: it writes a test file with it and keeps it. The key goes to the server only.",
		change(false, true)), model.RightServers, changes(services, tools.setBucketKey))
	Add(server, newTool("set_default_bucket", "Make a bucket the default",
		"Makes a bucket the workspace's default: apps without a bucket of their own back up there from now on; their copies start again there; what is in the old default stays.",
		change(false, true)), model.RightServers, changes(services, tools.setDefaultBucket))
	Add(server, newTool("remove_bucket", "Remove a bucket",
		"Removes a bucket from moin; what is in it stays. A bucket that apps use, or that apps left their copies in, is refused with bucket_in_use, naming the apps. Ask the user first: confirm must be the bucket's name, typed out.",
		change(true, true)), model.RightDelete, changes(services, tools.removeBucket))
	Add(server, newTool("set_app_bucket", "Choose an app's bucket",
		"Moves an app's backups to a bucket of the workspace; an empty bucket follows the workspace's default. Its copies start again in the new bucket; what is in the old one stays.",
		change(false, true)), model.RightDeploy, changes(services, tools.setAppBucket))
}

// RemoveBucketInput is the input of remove_bucket.
type RemoveBucketInput struct {
	Name    string `json:"name" jsonschema:"the bucket's name in moin"`
	Confirm string `json:"confirm" jsonschema:"the bucket's name, typed out, after the user agreed"`
}

// BucketNameInput names one bucket.
type BucketNameInput struct {
	Name string `json:"name" jsonschema:"the bucket's name in moin"`
}

// AddBucketInput is the input of add_bucket.
type AddBucketInput struct {
	Server    string `json:"server,omitempty" jsonschema:"the server that checks the bucket and keeps the secret key; empty for the workspace's only server"`
	Name      string `json:"name" jsonschema:"a name for the bucket in moin: lowercase letters, digits, dashes"`
	Endpoint  string `json:"endpoint,omitempty" jsonschema:"https:// URL of an S3-compatible store; empty for AWS S3"`
	Region    string `json:"region,omitempty" jsonschema:"the region; auto for Cloudflare R2, the default"`
	Bucket    string `json:"bucket" jsonschema:"the bucket's name at the bucket provider"`
	Path      string `json:"path,omitempty" jsonschema:"a folder inside the bucket, only if it holds other files too"`
	AccessKey string `json:"access_key" jsonschema:"the access key ID"`
	SecretKey string `json:"secret_key" jsonschema:"the secret key; it goes to the server, never back"`
	Default   bool   `json:"default,omitempty" jsonschema:"make it the workspace's default bucket"`
}

// EditBucketInput is the input of edit_bucket.
type EditBucketInput struct {
	Server    string `json:"server,omitempty" jsonschema:"the server that checks a new place or key and keeps the key; empty for the workspace's only server"`
	Name      string `json:"name" jsonschema:"the bucket's current name in moin"`
	Rename    string `json:"rename,omitempty" jsonschema:"a new name in moin; empty keeps it"`
	Endpoint  string `json:"endpoint,omitempty" jsonschema:"https:// URL of an S3-compatible store; empty for AWS S3"`
	Region    string `json:"region,omitempty" jsonschema:"the region; auto for Cloudflare R2"`
	Bucket    string `json:"bucket" jsonschema:"the bucket's name at the bucket provider"`
	Path      string `json:"path,omitempty" jsonschema:"a folder inside the bucket; empty for none"`
	AccessKey string `json:"access_key" jsonschema:"the access key ID"`
	SecretKey string `json:"secret_key,omitempty" jsonschema:"a new secret key; empty keeps the one the server has"`
	Default   bool   `json:"default,omitempty" jsonschema:"make it the workspace's default bucket"`
}

// BucketKeyInput is the input of set_bucket_key.
type BucketKeyInput struct {
	Name      string `json:"name" jsonschema:"the bucket's name in moin"`
	Server    string `json:"server,omitempty" jsonschema:"the server that keeps the key; empty for the workspace's only server"`
	SecretKey string `json:"secret_key" jsonschema:"the secret key; it goes to the server, never back"`
}

// AppBucketInput is the input of set_app_bucket.
type AppBucketInput struct {
	App    string `json:"app" jsonschema:"the app name or ID"`
	Bucket string `json:"bucket" jsonschema:"the bucket's name in moin; empty follows the workspace's default"`
}

// BucketList is the output of list_buckets.
type BucketList struct {
	api.BucketList
}

// Bucket is the output of add_bucket, edit_bucket and set_default_bucket.
type Bucket struct {
	bucket
}

// bucket is api.Bucket under another name: embedded as api.Bucket, its
// field bucket would hide behind the embedded field Bucket in the output
// schema.
type bucket = api.Bucket

func (t moinTools) listBuckets(_ context.Context, caller Caller, _ struct{}) (BucketList, error) {
	buckets, err := t.services.Apps.Buckets(caller.Bearer.WorkspaceID)
	if err != nil {
		return BucketList{}, err
	}
	list := api.BucketList{Items: make([]api.Bucket, 0, len(buckets))}
	for _, bucket := range buckets {
		list.Items = append(list.Items, api.NewBucket(bucket))
	}
	return BucketList{list}, nil
}

func (t moinTools) addBucket(ctx context.Context, caller Caller, input AddBucketInput) (Bucket, error) {
	bucket, err := t.services.Apps.AddBucket(ctx, caller.actor(), service.BucketInput(input))
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{api.NewBucket(*bucket)}, nil
}

func (t moinTools) editBucket(ctx context.Context, caller Caller, input EditBucketInput) (Bucket, error) {
	bucket, err := t.services.Apps.EditBucket(ctx, caller.actor(), input.Name, service.BucketInput{
		Server: input.Server, Name: input.Rename, Endpoint: input.Endpoint, Region: input.Region, Bucket: input.Bucket, Path: input.Path,
		AccessKey: input.AccessKey, SecretKey: input.SecretKey, Default: input.Default,
	})
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{api.NewBucket(*bucket)}, nil
}

func (t moinTools) setBucketKey(ctx context.Context, caller Caller, input BucketKeyInput) (Bucket, error) {
	bucket, err := t.services.Apps.SetBucketKey(ctx, caller.actor(), input.Name, input.Server, input.SecretKey)
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{api.NewBucket(*bucket)}, nil
}

func (t moinTools) setDefaultBucket(ctx context.Context, caller Caller, input BucketNameInput) (Bucket, error) {
	bucket, err := t.services.Apps.MakeDefaultBucket(ctx, caller.actor(), input.Name)
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{api.NewBucket(*bucket)}, nil
}

func (t moinTools) removeBucket(_ context.Context, caller Caller, input RemoveBucketInput) (Done, error) {
	bucket, err := t.services.Apps.Bucket(caller.Bearer.WorkspaceID, input.Name)
	if err != nil {
		return Done{}, err
	}
	err = t.services.Apps.RemoveBucket(caller.actor(), bucket.Name, input.Confirm)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Removed the bucket " + bucket.Name + ". What is in it stays."}, nil
}

func (t moinTools) setAppBucket(_ context.Context, caller Caller, input AppBucketInput) (App, error) {
	app, err := t.services.Apps.SetAppBucket(caller.actor(), input.App, input.Bucket)
	if err != nil {
		return App{}, err
	}
	return App{api.NewApp(*app)}, nil
}
