package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/protocol"
)

// The buckets page shows every bucket with the apps that use it, marks
// the default and offers to make another one the default.
func TestBucketsPage(t *testing.T) {
	view := BucketView{Buckets: []service.Bucket{
		{Name: "default", Default: true, Bucket: "acme", Folder: "workspace-1/", Apps: []string{"blog"}, Older: []string{"shop"}},
		{Name: "client-a", Bucket: "client", Folder: "workspace-1/", Apps: []string{"shop"}},
	}}
	var html strings.Builder
	if err := Bucket(view).Render(projectPageContext("/app/tiny-rocket-co/bucket"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{`data-backup-storage="default"`, `data-backup-storage="client-a"`, "data-default-bucket", `data-make-default="client-a"`, "/bucket/client-a/remove", ">shop</span>", "data-bucket-older", "Another bucket"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page has no %q", want)
		}
	}
	if strings.Contains(page, `data-make-default="default"`) {
		t.Error("the default bucket offers to become the default")
	}
}

// The backups tab picks the app's bucket among several and marks a
// snapshot in an older bucket, whose restore names that bucket.
func TestBackupsPickTheBucket(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	current := service.Bucket{Name: "client-a", Bucket: "client", Folder: "workspace-1/"}
	view := BackupsView{
		App:     model.App{ID: "app-1", Name: "shop", BucketName: "client-a"},
		Storage: &current,
		Buckets: []service.Bucket{{Name: "default", Default: true}, current},
		Older:   []service.Bucket{{Name: "default"}},
		Now:     now,
		Backups: protocol.Backups{App: "shop", Databases: []protocol.DatabaseBackup{{Database: "/data/app.db", Snapshots: []protocol.Snapshot{
			{ID: "20261103T110000Z-manual", Reason: "manual", CreatedAt: now.Add(-time.Hour), Bucket: "client-a"},
			{ID: "20261101T110000Z-manual", Reason: "manual", CreatedAt: now.Add(-49 * time.Hour), Bucket: "default"},
		}}}},
	}
	var html strings.Builder
	if err := AppBackups(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop/backups"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{`id="app-bucket-form"`, "The default (default)", `data-snapshot-bucket="default"`, `name="bucket" value="default"`, "data-bucket-older"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page has no %q", want)
		}
	}
	if strings.Contains(page, `data-snapshot-bucket="client-a"`) {
		t.Error("a snapshot in the app's own bucket is marked as older")
	}
}
