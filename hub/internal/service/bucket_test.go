package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/protocol"
)

// answerBackupConfigs answers every backup config the daemon gets, so a
// change waits for no one.
func answerBackupConfigs(t *testing.T, servers *ServerService, serverID string, daemon *fakeDaemon) {
	t.Helper()
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		answered := map[string]bool{}
		for {
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond):
			}
			var open []string
			daemon.mu.Lock()
			for _, sent := range daemon.sent {
				if sent.Type == protocol.TypeBackupConfig && !answered[sent.RequestID] {
					answered[sent.RequestID] = true
					open = append(open, sent.RequestID)
				}
			}
			daemon.mu.Unlock()
			for _, id := range open {
				message, _ := protocol.NewMessage(protocol.TypeOK, nil)
				message.RequestID = id
				servers.Receive(serverID, message)
			}
		}
	}()
}

// TestBuckets: the bucket a workspace had stays its default, an app picks
// another one, the daemon gets both in the old shape plus the app's own,
// and a bucket in use is not removed.
func TestBuckets(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	answerBackupConfigs(t, servers, server.ID, daemon)
	ctx := context.Background()
	endpoint := "https://s3.example.com"
	blog, err := apps.Create(owner, "blog", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(owner, "shop", server.ID); err != nil {
		t.Fatal(err)
	}

	// The default serves every app; nothing changes for the daemon.
	current, err := apps.AppBucket(blog)
	if err != nil || current.Name != "default" || !current.Default {
		t.Fatalf("blog's bucket = %+v, %v", current, err)
	}
	config, _ := apps.backupConfig(owner.WorkspaceID, server.ID)
	if config.Bucket == nil || config.Bucket.Bucket != "owner" || config.Bucket.ID != "bucket-owner" || len(config.AppBuckets) != 0 || len(config.Paused) != 0 {
		t.Fatalf("config with one bucket = %+v", config)
	}

	// A second bucket is not the default; a bad name or a taken one is
	// refused.
	input := BucketInput{Name: "client-a", Endpoint: endpoint, Bucket: "client", AccessKey: "key-a", SecretKey: "secret-a"}
	added, err := apps.AddBucket(ctx, owner, input)
	if err != nil || added.Default || added.Region != "auto" || len(added.Servers.On) != 1 || added.Servers.On[0].Server != "web-1" {
		t.Fatalf("add client-a = %+v, %v", added, err)
	}
	// The server checked it and keeps the key.
	var set protocol.BucketSet
	daemon.last(t, protocol.TypeBucketSet, &set)
	if set.SecretKey != "secret-a" || set.Bucket.ID == "" || set.Bucket.Bucket != "client" || set.Bucket.Workspace != owner.WorkspaceID {
		t.Fatalf("bucket.set = %+v", set)
	}
	if _, err := apps.AddBucket(ctx, owner, input); !errors.Is(err, repository.ErrDuplicateBucket) {
		t.Fatalf("add client-a twice: err = %v", err)
	}
	bad := input
	bad.Name = "Client A"
	if _, err := apps.AddBucket(ctx, owner, bad); !errors.Is(err, ErrBucketName) {
		t.Fatalf("add a bad name: err = %v", err)
	}

	// blog moves: its backups go to client-a, shop stays on the
	// default, and the daemon still gets the default as Bucket.
	moved, err := apps.SetAppBucket(owner, "blog", "client-a")
	if err != nil || moved.BucketName != "client-a" {
		t.Fatalf("move blog = %+v, %v", moved, err)
	}
	config, _ = apps.backupConfig(owner.WorkspaceID, server.ID)
	if config.Bucket == nil || config.Bucket.Bucket != "owner" {
		t.Fatalf("the default for old daemons = %+v", config.Bucket)
	}
	if own := config.AppBuckets["blog"]; own == nil || own.Bucket != "client" || own.ID != set.Bucket.ID || own.Workspace != owner.WorkspaceID || own.Name != "client-a" {
		t.Fatalf("blog's own bucket = %+v", config.AppBuckets)
	}
	if _, ok := config.AppBuckets["shop"]; ok {
		t.Fatal("shop got a bucket of its own")
	}
	if shown, err := apps.Bucket(owner.WorkspaceID, "client-a"); err != nil || shown.AppFolder(blog) != owner.WorkspaceID+"/blog/"+blog.ID+"/" {
		t.Fatalf("blog's folder in client-a = %+v, %v", shown, err)
	}
	older, err := apps.OlderBuckets(moved)
	if err != nil || len(older) != 1 || older[0].Name != "default" {
		t.Fatalf("blog's older buckets = %+v, %v", older, err)
	}

	// In use: client-a by blog, the default by shop and blog's older backups.
	var inUse *BucketInUseError
	if err := apps.RemoveBucket(owner, "client-a", "client-a"); !errors.As(err, &inUse) || len(inUse.Apps) != 1 || inUse.Apps[0] != "blog" {
		t.Fatalf("remove client-a: err = %v", err)
	}
	if err := apps.RemoveBucket(owner, "default", "default"); !errors.As(err, &inUse) || len(inUse.Apps) != 1 || inUse.Apps[0] != "shop" || len(inUse.Older) != 1 || inUse.Older[0] != "blog" {
		t.Fatalf("remove default: err = %v", err)
	}

	// An unused bucket goes.
	input.Name = "spare"
	if _, err := apps.AddBucket(ctx, owner, input); err != nil {
		t.Fatal(err)
	}
	for _, confirm := range []string{"", "Spare", "default"} {
		if err := apps.RemoveBucket(owner, "spare", confirm); !errors.Is(err, ErrConfirmMismatch) {
			t.Fatalf("remove with confirm %q: err = %v", confirm, err)
		}
	}
	if _, err := apps.Bucket(owner.WorkspaceID, "spare"); err != nil {
		t.Fatalf("a refused confirm removed the bucket: %v", err)
	}
	if err := apps.RemoveBucket(owner, "spare", "spare"); err != nil {
		t.Fatalf("remove an unused bucket: %v", err)
	}
	if _, err := apps.Bucket(owner.WorkspaceID, "spare"); !errors.Is(err, repository.ErrBucketNotFound) {
		t.Fatalf("spare after removal: err = %v", err)
	}

	// A new default takes shop along; shop's older backups stay in the old
	// one. blog back on the default finds its older backups current.
	if _, err := apps.MakeDefaultBucket(ctx, owner, "client-a"); err != nil {
		t.Fatal(err)
	}
	shop, _ := apps.Get(owner.WorkspaceID, "shop")
	if current, _ := apps.AppBucket(shop); current.Name != "client-a" {
		t.Fatalf("shop after the new default = %s", current.Name)
	}
	if older, _ := apps.OlderBuckets(shop); len(older) != 1 || older[0].Name != "default" {
		t.Fatalf("shop's older buckets = %+v", older)
	}
	buckets, err := apps.Buckets(owner.WorkspaceID)
	if err != nil || len(buckets) != 2 || buckets[0].Name != "client-a" || !buckets[0].Default || buckets[1].Default {
		t.Fatalf("buckets = %+v, %v", buckets, err)
	}

	// Renaming asks no server: the key stays where it is.
	checks := daemon.count(protocol.TypeBucketSet)
	renamed, err := apps.EditBucket(ctx, owner, "default", BucketInput{Name: "old", Endpoint: "https://s3.example.com", Bucket: "owner", AccessKey: "key"})
	if err != nil || renamed.Name != "old" || daemon.count(protocol.TypeBucketSet) != checks || len(renamed.Servers.On) != 1 {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
	// A new path is checked by the server, with the key it keeps.
	if _, err := apps.EditBucket(ctx, owner, "old", BucketInput{Endpoint: "https://s3.example.com", Bucket: "owner", Path: "moin", AccessKey: "key"}); err != nil {
		t.Fatal(err)
	}
	set = protocol.BucketSet{}
	daemon.last(t, protocol.TypeBucketSet, &set)
	if set.SecretKey != "" || set.Bucket.Path != "moin" {
		t.Fatalf("bucket.set for a new path = %+v", set)
	}
	// A bucket the server cannot write to is not stored.
	daemon.mu.Lock()
	daemon.refuse = map[protocol.MessageType]string{protocol.TypeBucketSet: "moin could not write to the bucket with these keys: AccessDenied"}
	daemon.mu.Unlock()
	input.Name = "refused"
	var daemonErr *DaemonError
	if _, err := apps.AddBucket(ctx, owner, input); !errors.As(err, &daemonErr) || !strings.Contains(err.Error(), "could not write") {
		t.Fatalf("a refused bucket: err = %v", err)
	}
	if _, err := apps.Bucket(owner.WorkspaceID, "refused"); !errors.Is(err, repository.ErrBucketNotFound) {
		t.Fatalf("a refused bucket was stored: %v", err)
	}
}

// TestFirstBucketIsTheDefault: a workspace without a bucket gets its
// first as the default, and the last bucket may go once no app uses it.
func TestFirstBucketIsTheDefault(t *testing.T) {
	apps, servers, _, _ := newAppService(t)
	ctx := context.Background()
	input := BucketInput{Name: "first", Endpoint: "https://s3.example.com", Bucket: "other", AccessKey: "key", SecretKey: "secret"}
	// Without a server there is no one to keep the key.
	if _, err := apps.AddBucket(ctx, other, input); !errors.Is(err, ErrNoServer) {
		t.Fatalf("a bucket without a server: err = %v", err)
	}
	connectServer(t, servers, other, "other-1")
	if _, err := apps.AddBucket(ctx, other, input); err != nil {
		t.Fatal(err)
	}
	bucket, err := apps.DefaultBucket(other.WorkspaceID)
	if err != nil || bucket.Name != "first" || !bucket.Default {
		t.Fatalf("first bucket = %+v, %v", bucket, err)
	}
	// Without apps the last bucket may go.
	if err := apps.RemoveBucket(other, "first", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.DefaultBucket(other.WorkspaceID); !errors.Is(err, ErrNoBackups) {
		t.Fatalf("after removing the last bucket: err = %v", err)
	}
}
