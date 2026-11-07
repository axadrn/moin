package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/internal/link"
	"github.com/axadrn/moin/protocol"
)

// TestRenameApp: a renamed app keeps its folder, so its server, its
// backups and its instant address know it as before, while its
// containers take the new name; links count only its ID; an app
// created later with the old name gets a folder of its own.
func TestRenameApp(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	app, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if app.Folder != "blog" {
		t.Fatalf("folder = %q, want the name", app.Folder)
	}
	first, err := deployUpload(apps)
	if err != nil {
		t.Fatal(err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: first.ID, OK: true, Detected: protocol.Detected{Kind: "go", Service: "web", Port: 8080}})

	if _, err := apps.Rename(owner, "blog", "Journal"); !errors.Is(err, ErrAppName) {
		t.Fatalf("upper case name: err = %v, want ErrAppName", err)
	}
	if _, err := apps.Create(owner, "notes", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Rename(owner, "blog", "notes"); !errors.Is(err, repository.ErrDuplicateApp) {
		t.Fatalf("taken name: err = %v, want ErrDuplicateApp", err)
	}
	if _, err := apps.Rename(other, "blog", "journal"); !errors.Is(err, repository.ErrAppNotFound) {
		t.Fatalf("another workspace renamed it: err = %v", err)
	}
	renamed, err := apps.Rename(owner, link.To("blog", app.ID), "journal")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != app.ID || renamed.Name != "journal" || renamed.Folder != "blog" {
		t.Fatalf("renamed = %s %s %s", renamed.ID, renamed.Name, renamed.Folder)
	}

	// Name, ID, UUID and links with any name find it; the old name and an
	// unknown ID do not.
	for _, ref := range []string{"journal", app.ID, link.ID(app.ID), link.To("journal", app.ID), link.To("blog", app.ID), link.To("anything-else", app.ID)} {
		got, err := apps.Get(owner.WorkspaceID, ref)
		if err != nil || got.ID != app.ID {
			t.Fatalf("Get(%q) = %v, %v", ref, got, err)
		}
	}
	for _, ref := range []string{"blog", link.To("journal", "00000000-0000-0000-0000-000000000000"), "journal-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5"} {
		if _, err := apps.Get(owner.WorkspaceID, ref); !errors.Is(err, repository.ErrAppNotFound) {
			t.Fatalf("Get(%q): err = %v, want ErrAppNotFound", ref, err)
		}
	}

	// The rename waits for the next restart, which names the app on the
	// server by its folder and gives the containers the new name.
	_, deploys, err := apps.Deploys(owner.WorkspaceID, "journal", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := apps.Get(owner.WorkspaceID, "journal"); len(deploys) != 1 {
		t.Fatalf("deploys = %d", len(deploys))
	} else if waiting, err := apps.ChangesWaiting(owner.WorkspaceID, got); err != nil || waiting != 1 {
		t.Fatalf("changes waiting = %d, %v; want the rename", waiting, err)
	}
	if _, err := apps.Restart(owner, "journal"); err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.App != "blog" || start.Name != "journal" || start.AppID != app.ID {
		t.Fatalf("deploy.start names %q, %q, %q", start.App, start.Name, start.AppID)
	}

	// What the daemon reports by the folder lands on the renamed app.
	daemonSays(t, servers, server.ID, protocol.TypeMetrics, protocol.Metrics{Apps: []protocol.AppMetrics{{App: "blog", MemoryUsed: 42 << 20, Containers: 1}}})
	_, metrics, err := apps.AppMetrics(owner.WorkspaceID, "journal", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Now == nil || metrics.Now.MemoryUsed != 42<<20 {
		t.Fatalf("metrics of the renamed app = %+v", metrics.Now)
	}
	config := apps.backupConfig(owner.WorkspaceID, server.ID)
	if _, ok := config.Schedules["blog"]; !ok {
		t.Fatalf("backup schedules = %v, want the folder", config.Schedules)
	}
	if ids := apps.appIDs(server.ID, owner.WorkspaceID); ids["blog"] != app.ID {
		t.Fatalf("app IDs = %v", ids)
	}
	go func() {
		daemonAnswers(servers, server.ID, daemon, protocol.TypeLogs, protocol.TypeLogsResult, protocol.LogsResult{Lines: []string{"hello"}})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := apps.Logs(ctx, owner.WorkspaceID, "journal", 10); err != nil {
		t.Fatal(err)
	}
	var logs protocol.LogsRequest
	daemon.last(t, protocol.TypeLogs, &logs)
	if logs.App != "blog" {
		t.Fatalf("logs asked for %q, want the folder", logs.App)
	}

	// A new app with the old name gets a folder of its own.
	again, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Folder != "blog-2" {
		t.Fatalf("folder of the new blog = %q, want blog-2", again.Folder)
	}
	if got, err := apps.Get(owner.WorkspaceID, "blog"); err != nil || got.ID != again.ID {
		t.Fatalf("blog is now %v, %v", got, err)
	}
}

// TestBucketAndServerByLink: a bucket and a server are found by a link with
// any name, their ID or their name; not by an unknown ID.
func TestBucketAndServerByLink(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	const id = "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	if err := apps.apps.CreateBucket(model.Bucket{ID: id, WorkspaceID: owner.WorkspaceID, Name: "client-a", Endpoint: "https://s3.example.com", Region: "auto", Bucket: "a", AccessKey: "k", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"client-a", id, link.ID(id), link.To("client-a", id), link.To("old-name", id)} {
		if bucket, err := apps.Bucket(owner.WorkspaceID, ref); err != nil || bucket.ID != id {
			t.Fatalf("bucket %q = %v, %v", ref, bucket, err)
		}
	}
	if _, err := apps.Bucket(owner.WorkspaceID, link.To("client-a", "00000000-0000-4000-8000-000000000000")); !errors.Is(err, repository.ErrBucketNotFound) {
		t.Fatalf("unknown ID: err = %v", err)
	}
	if _, err := apps.Bucket(other.WorkspaceID, link.To("client-a", id)); !errors.Is(err, repository.ErrBucketNotFound) {
		t.Fatalf("another workspace found it: err = %v", err)
	}
	for _, ref := range []string{"web-1", server.ID, link.To("web-9", server.ID)} {
		if got, err := servers.Get(owner.WorkspaceID, ref); err != nil || got.ID != server.ID {
			t.Fatalf("server %q = %v, %v", ref, got, err)
		}
	}
	if _, err := servers.Get(other.WorkspaceID, link.To("web-1", server.ID)); !errors.Is(err, repository.ErrServerNotFound) {
		t.Fatalf("another workspace found the server: err = %v", err)
	}
}
