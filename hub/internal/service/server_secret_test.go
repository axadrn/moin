package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

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
// bucket's secret key, a Tailscale key and the app's password go to the
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
	if _, err := apps.RemovePassword(owner, "blog"); err != nil {
		t.Fatal(err)
	}
	made, _, err := apps.SetPassword(owner, "blog", "", "")
	if err != nil || made == "" {
		t.Fatalf("a new password: %q, %v", made, err)
	}
	tailscale := "tskey-auth-5c2e91"
	secrets := []string{"env-value-7f3a91", "registry-password-9c1d44", "s3-value-3e8b27", tailscale, app.NewPassword, made}
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

// TestPasswordShownOnce: a new app starts with its password on, user moin:
// moin shows the password it made once and keeps only its bcrypt hash,
// which every router of the app gets. Turned off, the deploy carries none;
// on again without a password, moin makes a new one and shows it once.
func TestPasswordShownOnce(t *testing.T) {
	apps, _, _, daemon := newAppService(t)
	app, err := apps.Create(owner, "blog", "web-1")
	if err != nil || len(app.NewPassword) != 20 {
		t.Fatalf("created %+v, %v", app, err)
	}
	stored, _ := apps.Get(owner.WorkspaceID, "blog")
	user, hash, _ := strings.Cut(stored.Password, ":")
	if stored.NewPassword != "" || user != PasswordUser || bcrypt.CompareHashAndPassword([]byte(hash), []byte(app.NewPassword)) != nil {
		t.Fatalf("stored %q, password %q", stored.NewPassword, stored.Password)
	}
	// The deploy hands the server the hash, never the password, for
	// its domains and its instant address: one line, password.
	if _, err := deploySource(apps); err != nil {
		t.Fatal(err)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Domains.Password != stored.Password || strings.Contains(fmt.Sprint(start), app.NewPassword) {
		t.Fatalf("deploy.start = %+v", start.Domains)
	}
	if changed, err := apps.RemovePassword(owner, "blog"); err != nil || !changed {
		t.Fatalf("off: %v, %v", changed, err)
	}
	again, changed, err := apps.SetPassword(owner, "blog", "", "")
	if err != nil || !changed || len(again) != 20 || again == app.NewPassword {
		t.Fatalf("on again: %q, %v, %v", again, changed, err)
	}
	renewed, _ := apps.Get(owner.WorkspaceID, "blog")
	if _, hash, _ := strings.Cut(renewed.Password, ":"); bcrypt.CompareHashAndPassword([]byte(hash), []byte(again)) != nil {
		t.Fatal("the new hash does not take the new password")
	}
	// On while on changes nothing: no password is replaced by surprise.
	if made, changed, err := apps.SetPassword(owner, "blog", "", ""); err != nil || changed || made != "" {
		t.Fatalf("on while on: %q, %v, %v", made, changed, err)
	}
	if kept, _ := apps.Get(owner.WorkspaceID, "blog"); kept.Password != renewed.Password {
		t.Fatal("on while on replaced the password")
	}
	if _, _, err := apps.SetPassword(owner, "blog", "a,b", "a-long-password"); !errors.Is(err, ErrPasswordUser) {
		t.Fatalf("a comma in the username: %v", err)
	}
}

// TestServerIsTheTruthForEnvKeys: the hub shows the env keys a server
// reports, in its hello and in its answer to env.set: a key the server
// lacks goes, one it has, such as from an env file put back by hand,
// comes, and an app it reports nothing for has none.
func TestServerIsTheTruthForEnvKeys(t *testing.T) {
	apps, servers, server, daemon := newAppService(t)
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(owner, "shop", "web-1"); err != nil {
		t.Fatal(err)
	}
	keys := func(app string) []string {
		vars, _ := apps.EnvVars(owner.WorkspaceID, app)
		names := []string{}
		for _, envVar := range vars {
			names = append(names, envVar.Key)
		}
		return names
	}
	if _, err := apps.SetEnv(context.Background(), owner, "blog", map[string]string{"A": "1", "B": "2"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := keys("blog"); !slices.Equal(got, []string{"A", "B"}) {
		t.Fatalf("after env.set = %v", got)
	}
	changed := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	hello := testHello
	hello.Env = map[string]protocol.AppEnv{"blog": {Keys: []string{"B", "C", "not a key"}, ChangedAt: changed}}
	servers.Connect(server, hello, "203.0.113.5", func() {}, func(message protocol.Message) error {
		daemon.take(servers, server.ID, message)
		return nil
	})
	if got := keys("blog"); !slices.Equal(got, []string{"B", "C"}) {
		t.Fatalf("after the hello = %v", got)
	}
	vars, _ := apps.EnvVars(owner.WorkspaceID, "blog")
	if !vars[1].UpdatedAt.Equal(changed) || vars[0].UpdatedAt.Equal(changed) {
		t.Fatalf("dates = %+v", vars)
	}
	if got := keys("shop"); len(got) != 0 {
		t.Fatalf("an app the server reported nothing for = %v", got)
	}
}
