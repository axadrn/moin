package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

// databaseHolds says whether the hub's database file holds value
// anywhere, in a table, an index or the write-ahead log.
func databaseHolds(t *testing.T, database *sqlx.DB, value string) bool {
	t.Helper()
	var file string
	if err := database.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&file); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range []string{file, file + "-wal"} {
		body, err := os.ReadFile(name)
		if err == nil && bytes.Contains(body, []byte(value)) {
			found = true
		}
	}
	return found
}

// TestSecretsNeverReachTheHubDatabase: env values, a registry password, a
// bucket's secret key, a Tailscale key and the instant password go to the
// server, or to the person once; the hub's database holds none of them,
// only names and dates.
func TestSecretsNeverReachTheHubDatabase(t *testing.T) {
	apps, _, _, daemon, database := newAppServiceOn(t)
	ctx := context.Background()
	answerBackupConfigs(t, apps.servers, mustServer(t, apps, "web-1"), daemon)
	app, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	instant, err := apps.NewInstantPassword(owner, "blog")
	if err != nil {
		t.Fatal(err)
	}
	tailscale := "tskey-auth-5c2e91"
	secrets := []string{"env-value-7f3a91", "registry-password-9c1d44", "s3-value-3e8b27", tailscale, app.InstantPassword, instant}
	if _, err := apps.SetEnv(ctx, owner, "blog", map[string]string{"DATABASE_URL": secrets[0]}, nil); err != nil {
		t.Fatal(err)
	}
	if err := apps.SetRegistry(ctx, owner, "ghcr.io", "ada", secrets[1], ""); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.AddBucket(ctx, owner, BucketInput{Name: "client-a", Endpoint: "https://s3.example.com", Bucket: "client", AccessKey: "key-a", SecretKey: secrets[2]}); err != nil {
		t.Fatal(err)
	}
	if err := apps.SetIntegrations(ctx, owner, IntegrationsInput{TailscaleKey: &tailscale}); err != nil {
		t.Fatal(err)
	}
	if env := daemon.env(t, app.Folder); env["DATABASE_URL"] != secrets[0] {
		t.Fatalf("the server keeps %v", env)
	}
	if _, err := database.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	for _, value := range secrets {
		if databaseHolds(t, database, value) {
			t.Errorf("the hub's database holds %q", value)
		}
	}
	vars, _ := apps.EnvVars(owner.WorkspaceID, "blog")
	if len(vars) != 1 || vars[0].Key != "DATABASE_URL" {
		t.Errorf("the hub lists %+v", vars)
	}
}

func mustServer(t *testing.T, apps *AppService, name string) string {
	t.Helper()
	server, err := apps.servers.Get(owner.WorkspaceID, name)
	if err != nil {
		t.Fatal(err)
	}
	return server.ID
}

// TestOfflineServerTakesNoSecret: a value for an offline server is refused
// with its name, and the hub keeps nothing to send later. With two servers
// a server-wide secret names the one that keeps it.
func TestOfflineServerTakesNoSecret(t *testing.T) {
	apps, servers, _, _ := newAppService(t)
	ctx := context.Background()
	gone, _, err := servers.Create(owner, "web-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(owner, "shop", gone.ID); err != nil {
		t.Fatal(err)
	}
	_, err = apps.SetEnv(ctx, owner, "shop", map[string]string{"KEY": "value"}, nil)
	var offline *OfflineError
	if !errors.As(err, &offline) || !errors.Is(err, ErrServerOffline) || err.Error() != "web-2 is offline. Try again when it is back." {
		t.Fatalf("env for an offline server: err = %v", err)
	}
	if vars, _ := apps.EnvVars(owner.WorkspaceID, "shop"); len(vars) != 0 {
		t.Fatalf("the hub noted %+v", vars)
	}
	if err := apps.SetRegistry(ctx, owner, "ghcr.io", "ada", "token", ""); !errors.Is(err, ErrServerRequired) {
		t.Fatalf("a login without a server, with two: err = %v", err)
	}
	if err := apps.SetRegistry(ctx, owner, "ghcr.io", "ada", "token", "web-2"); !errors.As(err, &offline) {
		t.Fatalf("a login for an offline server: err = %v", err)
	}
	if logins, _ := apps.Registries(owner.WorkspaceID); len(logins) != 0 {
		t.Fatalf("a refused login was stored: %+v", logins)
	}
}

// TestServerWithoutKeyIsReported: a server added later lacks the bucket
// key and the registry login: the lists name it, its apps' backups pause
// with a reason, and an image deploy there says what to enter.
func TestServerWithoutKeyIsReported(t *testing.T) {
	apps, servers, _, _ := newAppService(t)
	ctx := context.Background()
	if err := apps.SetRegistry(ctx, owner, "ghcr.io", "ada", "token", ""); err != nil {
		t.Fatal(err)
	}
	later, laterDaemon := connectServer(t, servers, owner, "web-2")
	logins, _ := apps.Registries(owner.WorkspaceID)
	if len(logins) != 1 || !slices.Equal(logins[0].Servers.MissingOn, []string{"web-2"}) {
		t.Fatalf("registries = %+v", logins)
	}
	bucket, _ := apps.Bucket(owner.WorkspaceID, "default")
	if !slices.Equal(bucket.Servers.MissingOn, []string{"web-2"}) || len(bucket.Servers.On) != 1 {
		t.Fatalf("bucket servers = %+v", bucket.Servers)
	}
	if _, err := apps.Create(owner, "shop", later.ID); err != nil {
		t.Fatal(err)
	}
	config, err := apps.backupConfig(owner.WorkspaceID, later.ID)
	if err != nil || config.Paused["shop"] != "web-2 has no key for the bucket default. Enter it for web-2 under Backups." {
		t.Fatalf("backups on web-2 = %+v, %v", config, err)
	}
	var missing *MissingSecretError
	if _, err := apps.Deploy(ctx, owner, "shop", "ghcr.io/ada/shop:1", ""); !errors.As(err, &missing) || !strings.HasPrefix(err.Error(), "web-2 has no login for ghcr.io.") {
		t.Fatalf("an image deploy on web-2: err = %v", err)
	}

	// Entered for web-2, the key is there and its backups go on.
	answerBackupConfigs(t, servers, later.ID, laterDaemon)
	if _, err := apps.SetBucketKey(ctx, owner, "default", "web-2", "secret"); err != nil {
		t.Fatal(err)
	}
	if config, _ := apps.backupConfig(owner.WorkspaceID, later.ID); len(config.Paused) != 0 {
		t.Fatalf("still paused: %+v", config.Paused)
	}
	var set protocol.BucketSet
	laterDaemon.last(t, protocol.TypeBucketSet, &set)
	if set.SecretKey != "secret" || set.Bucket.ID != "bucket-owner" {
		t.Fatalf("bucket.set = %+v", set)
	}
}

// TestHelloSaysWhatTheServerKeeps: the names a daemon reports are what the
// hub shows, also after a rebuild lost them; an old bucket key is removed.
func TestHelloSaysWhatTheServerKeeps(t *testing.T) {
	apps, servers, _, _ := newAppService(t)
	server, daemon := connectServer(t, servers, owner, "web-2", protocol.BucketSecret("bucket-owner"), protocol.BucketSecret("gone"))
	if bucket, _ := apps.Bucket(owner.WorkspaceID, "default"); len(bucket.Servers.MissingOn) != 0 {
		t.Fatalf("bucket servers = %+v", bucket.Servers)
	}
	apps.forgetOldSecrets(server.ID, owner.WorkspaceID)
	var secret protocol.SecretSet
	daemon.last(t, protocol.TypeSecretSet, &secret)
	if secret != (protocol.SecretSet{Name: "bucket/gone"}) {
		t.Fatalf("an old bucket key stays: %+v", secret)
	}
	// Rebuilt: the daemon connects again and keeps nothing.
	servers.Connect(server, testHello, "203.0.113.6", func() {}, func(message protocol.Message) error { return nil })
	if bucket, _ := apps.Bucket(owner.WorkspaceID, "default"); !slices.Equal(bucket.Servers.MissingOn, []string{"web-2"}) {
		t.Fatalf("after a rebuild = %+v", bucket.Servers)
	}
}

// TestInstantPasswordShownOnce: moin shows the password of the instant
// address when it makes it, at the app's start and with a new one, and
// keeps only its bcrypt hash, which goes to the server.
func TestInstantPasswordShownOnce(t *testing.T) {
	apps, _, _, _ := newAppService(t)
	app, err := apps.Create(owner, "blog", "web-1")
	if err != nil || len(app.InstantPassword) != 20 {
		t.Fatalf("created %+v, %v", app, err)
	}
	stored, _ := apps.Get(owner.WorkspaceID, "blog")
	if stored.InstantPassword != "" || bcrypt.CompareHashAndPassword([]byte(stored.InstantPasswordHash), []byte(app.InstantPassword)) != nil {
		t.Fatalf("stored %q, hash %q", stored.InstantPassword, stored.InstantPasswordHash)
	}
	again, err := apps.NewInstantPassword(owner, "blog")
	if err != nil || again == app.InstantPassword {
		t.Fatalf("new password %q, %v", again, err)
	}
	renewed, _ := apps.Get(owner.WorkspaceID, "blog")
	if bcrypt.CompareHashAndPassword([]byte(renewed.InstantPasswordHash), []byte(again)) != nil {
		t.Fatal("the new hash does not take the new password")
	}
	// The deploy hands the server the hash, never the password.
	start := protocol.Domains{}
	if err := apps.fillInstant(renewed, &start); err != nil {
		t.Fatal(err)
	}
	if start.InstantProtect != "moin:"+renewed.InstantPasswordHash {
		t.Fatalf("instant protect = %q", start.InstantProtect)
	}
}
