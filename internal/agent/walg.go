package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/axadrn/ops/protocol"
)

// Continuous Postgres backups wrap WAL-G, the way SQLite's wrap Litestream:
// ops writes no backup logic of its own. For every Postgres service the
// agent runs a companion container next to it:
//
//   - in the Postgres container's network namespace, so it reaches the
//     server on 127.0.0.1, where the official image trusts replication: no
//     password, no change to the app's compose file;
//   - with the data volume read-only, for base backups;
//   - running wal-g wal-receive, which streams every change and uploads
//     each finished WAL segment. Once a minute, when there were writes, the
//     agent closes the current segment with pg_switch_wal(), so a quiet
//     database is in the bucket within a minute too.
//
// Base backups run daily and before every deploy; WAL-G deletes what the
// 7 day window no longer needs. Everything goes to
//
//	<path>/<workspace>/<app>/<app ID>/postgres-<service>/walg/
//
// WAL-G is a pinned release, checked against its SHA-256. It needs glibc:
// an Alpine image of Postgres gets snapshots only, and says so.
const (
	walgVersion   = "v3.0.9"
	walgTick      = time.Minute
	walgBaseEvery = 24 * time.Hour
	walgSlot      = "ops_walg"
)

// walgSHA256 are the checksums of the pinned release, by GOARCH.
var walgSHA256 = map[string]string{
	"arm64": "072eb4a4a119f817e1fa888c64a776b01e3453b2e0d967a67d149adb18fde652",
	"amd64": "d6795c663894d836ba20840bffe8970aed81bbe25d3151bd4951f6f9c362def9",
}

// walgBinaryPath, when set, is a WAL-G binary to use instead of the
// download; tests set it.
var walgBinaryPath string

// walgState is what the agent knows of one database's continuous backup.
type walgState struct {
	// lsn is the WAL position after the last switch, to see new writes.
	lsn string
	// based is when the newest base backup finished.
	based time.Time
	// unsupported says why the image runs snapshots only.
	unsupported string
}

// walg runs the companions and remembers their state.
type walg struct {
	mu     sync.Mutex
	states map[string]*walgState
	// binary guards the download, run one round of walgOnce.
	binary sync.Mutex
	run    sync.Mutex
}

func (b *backups) walgState(key string) *walgState {
	b.walg.mu.Lock()
	defer b.walg.mu.Unlock()
	if b.walg.states == nil {
		b.walg.states = map[string]*walgState{}
	}
	state := b.walg.states[key]
	if state == nil {
		state = &walgState{}
		b.walg.states[key] = state
	}
	return state
}

// walgBinary returns the pinned WAL-G for this machine, downloaded once and
// checked against its SHA-256.
func (b *backups) walgBinary(ctx context.Context) (string, error) {
	if walgBinaryPath != "" {
		return walgBinaryPath, nil
	}
	b.walg.binary.Lock()
	defer b.walg.binary.Unlock()
	sum, ok := walgSHA256[runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("no WAL-G for %s", runtime.GOARCH)
	}
	target := filepath.Join(filepath.Dir(b.apps.dir), "bin", "wal-g-"+walgVersion)
	if fileSHA256(target) == sum {
		return target, nil
	}
	arch := map[string]string{"arm64": "aarch64", "amd64": "amd64"}[runtime.GOARCH]
	url := "https://github.com/wal-g/wal-g/releases/download/" + walgVersion + "/wal-g-pg-22.04-" + arch
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("download WAL-G: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download WAL-G: %s", response.Status)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	temp := target + ".new"
	out, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, hash), io.LimitReader(response.Body, 200<<20))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil && hex.EncodeToString(hash.Sum(nil)) != sum {
		err = errors.New("download WAL-G: the checksum does not match")
	}
	if err != nil {
		os.Remove(temp)
		return "", err
	}
	return target, os.Rename(temp, target)
}

func fileSHA256(file string) string {
	in, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer in.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, in); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// caBundle is the host's CA certificates, which the companion needs for
// HTTPS to the bucket: the Postgres image has none.
func caBundle() string {
	for _, file := range []string{"/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt", "/etc/ssl/cert.pem"} {
		if _, err := os.Stat(file); err == nil {
			return file
		}
	}
	return ""
}

// pgData is where a Postgres container keeps its data: PGDATA and the mount
// that holds it.
type pgData struct {
	dir   string
	mount string // a docker -v source:destination
}

// pgDataOf reads PGDATA and its mount from a running Postgres container.
func (a *apps) pgDataOf(ctx context.Context, container string) (pgData, error) {
	out, err := a.output(ctx, "docker", "inspect", "-f", "{{json .Config.Env}}\n{{json .Mounts}}", container)
	if err != nil {
		return pgData{}, err
	}
	envJSON, mountsJSON, _ := strings.Cut(strings.TrimSpace(out), "\n")
	var env []string
	_ = json.Unmarshal([]byte(envJSON), &env)
	dir := "/var/lib/postgresql/data"
	for _, item := range env {
		if value, ok := strings.CutPrefix(item, "PGDATA="); ok && value != "" {
			dir = value
		}
	}
	var mounts []struct {
		Type, Name, Source, Destination string
	}
	_ = json.Unmarshal([]byte(mountsJSON), &mounts)
	for _, mount := range mounts {
		if dir != mount.Destination && !within(mount.Destination, dir) {
			continue
		}
		source := mount.Source
		if mount.Type == "volume" {
			source = mount.Name
		}
		return pgData{dir: dir, mount: source + ":" + mount.Destination}, nil
	}
	return pgData{}, fmt.Errorf("PGDATA %s is on no volume: its data would be lost with the container", dir)
}

// walgPrefix is the folder of a database's WAL-G backups in its bucket.
func (b *backups) walgPrefix(app string, database protocol.Database) (*protocol.BackupStorage, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	storage := b.storageFor(app)
	if storage == nil {
		return nil, "", errors.New("backups are not set up")
	}
	prefix, err := b.prefix(app, database.Path)
	if err != nil {
		return nil, "", err
	}
	copied := *storage
	return &copied, prefix + "/walg", nil
}

// walgEnv is the environment WAL-G reads, as an env file only root may
// read: the keys never show in ps. remove deletes the file.
func walgEnv(storage *protocol.BackupStorage, prefix string, target pgTarget, data pgData) (file string, remove func(), err error) {
	region := storage.Region
	if region == "" {
		region = "auto"
	}
	lines := []string{
		"AWS_ACCESS_KEY_ID=" + storage.AccessKey,
		"AWS_SECRET_ACCESS_KEY=" + storage.SecretKey,
		"AWS_REGION=" + region,
		"WALG_S3_PREFIX=s3://" + storage.Bucket + "/" + prefix,
		"WALG_COMPRESSION_METHOD=lz4",
		"WALG_SLOTNAME=" + walgSlot,
		"PGHOST=127.0.0.1",
		"PGPORT=5432",
		"PGUSER=" + target.user,
		"PGDATABASE=" + target.database,
		"PGDATA=" + data.dir,
		"SSL_CERT_FILE=/etc/ssl/certs/ops-ca.crt",
	}
	if storage.Endpoint != "" {
		lines = append(lines, "AWS_ENDPOINT="+storage.Endpoint, "AWS_S3_FORCE_PATH_STYLE=true")
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\n\r") {
			return "", nil, errors.New("a bucket setting holds a line break")
		}
	}
	out, err := os.CreateTemp("", "ops-walg-*.env")
	if err != nil {
		return "", nil, err
	}
	_, err = out.WriteString(strings.Join(lines, "\n") + "\n")
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(out.Name())
		return "", nil, err
	}
	return out.Name(), func() { os.Remove(out.Name()) }, nil
}

// walgRunArgs are the docker run arguments every WAL-G container shares:
// the binary, the CA bundle and the env file.
func walgRunArgs(binary, envFile string) []string {
	args := []string{"-v", binary + ":/usr/local/bin/wal-g:ro", "--env-file", envFile}
	if bundle := caBundle(); bundle != "" {
		args = append(args, "-v", bundle+":/etc/ssl/certs/ops-ca.crt:ro")
	}
	return args
}

// companionName is the companion container of a Postgres service.
func companionName(app, service string) string { return "ops-walg-" + app + "-" + service }

// walgTarget is where a companion sends to: the bucket, folder and keys,
// hashed, so a change of bucket starts a new companion.
func walgTarget(storage *protocol.BackupStorage, prefix, container string) string {
	sum := sha256.Sum256([]byte(storage.Endpoint + "|" + storage.Bucket + "|" + prefix + "|" + storage.AccessKey + "|" + storage.SecretKey + "|" + container))
	return hex.EncodeToString(sum[:8])
}

// walgEnsure runs the companion of a Postgres service: it starts one when
// none runs, or when the one running follows another container or bucket.
// A new companion takes a base backup when the bucket has none yet.
func (b *backups) walgEnsure(ctx context.Context, app string, database protocol.Database) error {
	key := drillKey(app, database)
	state := b.walgState(key)
	target, err := b.apps.postgres(ctx, app, database)
	if err != nil {
		return err
	}
	storage, prefix, err := b.walgPrefix(app, database)
	if err != nil {
		return err
	}
	name := companionName(app, database.Service)
	want := walgTarget(storage, prefix, target.container)
	running, _ := b.apps.output(ctx, "docker", "inspect", "-f", `{{.State.Running}} {{index .Config.Labels "ops.walg.target"}}`, name)
	if strings.TrimSpace(running) == "true "+want {
		return nil
	}
	_, _ = b.apps.output(ctx, "docker", "rm", "-f", name)
	binary, err := b.walgBinary(ctx)
	if err != nil {
		return err
	}
	if out, err := b.apps.output(ctx, "docker", "run", "--rm", "--entrypoint", "/usr/local/bin/wal-g", "-v", binary+":/usr/local/bin/wal-g:ro", target.image, "--version"); err != nil {
		b.walg.mu.Lock()
		state.unsupported = "Point in time needs a Debian-based Postgres image, such as postgres:17: WAL-G does not run in " + target.image + ". It has snapshots."
		b.walg.mu.Unlock()
		return fmt.Errorf("%s: %s", state.unsupported, strings.TrimSpace(out))
	}
	b.walg.mu.Lock()
	state.unsupported = ""
	b.walg.mu.Unlock()
	data, err := b.apps.pgDataOf(ctx, target.container)
	if err != nil {
		return err
	}
	envFile, remove, err := walgEnv(storage, prefix, target, data)
	if err != nil {
		return err
	}
	defer remove()
	args := []string{"run", "-d", "--name", name,
		"--label", "ops.walg=" + app + "/" + database.Service, "--label", "ops.walg.target=" + want,
		"--network", "container:" + target.container,
		"--entrypoint", "/usr/local/bin/wal-g",
		"-v", data.mount + ":ro"}
	args = append(args, walgRunArgs(binary, envFile)...)
	args = append(args, target.image, "wal-receive")
	if _, err := b.apps.output(ctx, "docker", args...); err != nil {
		return fmt.Errorf("start WAL-G: %w", err)
	}
	slog.Info("backup: continuous Postgres backup", "app", app, "database", database.Path)
	newest, err := b.walgBaseBackups(ctx, app, database)
	if err == nil && len(newest) == 0 {
		return b.walgBase(ctx, app, database)
	}
	if err == nil && len(newest) > 0 {
		b.walg.mu.Lock()
		state.based = newest[len(newest)-1].at
		b.walg.mu.Unlock()
	}
	return nil
}

// walgBase takes a base backup with the companion, then lets WAL-G delete
// what the point in time window no longer needs.
func (b *backups) walgBase(ctx context.Context, app string, database protocol.Database) error {
	name := companionName(app, database.Service)
	target, err := b.apps.postgres(ctx, app, database)
	if err != nil {
		return err
	}
	data, err := b.apps.pgDataOf(ctx, target.container)
	if err != nil {
		return err
	}
	if out, err := b.apps.output(ctx, "docker", "exec", name, "/usr/local/bin/wal-g", "backup-push", data.dir); err != nil {
		return fmt.Errorf("base backup: %v %s", err, out)
	}
	state := b.walgState(drillKey(app, database))
	b.walg.mu.Lock()
	state.based = time.Now().UTC()
	b.walg.mu.Unlock()
	// The base backup ends in an open WAL segment: close it, so the backup
	// can be restored as soon as wal-receive uploads it.
	if err := b.walgSwitch(ctx, app, database); err != nil {
		slog.Warn("backup: switch WAL after the base backup", "app", app, "database", database.Path, "error", err)
	}
	cutoff := time.Now().Add(-pointInTime).UTC().Format(time.RFC3339)
	if _, err := b.apps.output(ctx, "docker", "exec", name, "/usr/local/bin/wal-g", "delete", "before", "FIND_FULL", cutoff, "--confirm"); err != nil {
		slog.Warn("backup: WAL-G retention", "app", app, "database", database.Path, "error", err)
	}
	slog.Info("backup: Postgres base backup", "app", app, "database", database.Path)
	return nil
}

// walgSwitch closes the current WAL segment when there were writes since
// the last switch, so wal-receive uploads it.
func (b *backups) walgSwitch(ctx context.Context, app string, database protocol.Database) error {
	target, err := b.apps.postgres(ctx, app, database)
	if err != nil {
		return err
	}
	state := b.walgState(drillKey(app, database))
	b.walg.mu.Lock()
	last := state.lsn
	b.walg.mu.Unlock()
	if last == "" {
		last = "0/0"
	}
	out, err := target.psql(ctx, `SELECT CASE WHEN pg_current_wal_lsn() <> :'last'::pg_lsn THEN pg_switch_wal() END;
SELECT pg_current_wal_lsn();`, map[string]string{"last": last}, false)
	if err != nil {
		return err
	}
	lines := strings.Fields(out)
	if len(lines) > 0 {
		b.walg.mu.Lock()
		state.lsn = lines[len(lines)-1]
		b.walg.mu.Unlock()
	}
	return nil
}

// walgLoop keeps the companions running, closes WAL segments with writes
// once a minute and takes the daily base backup.
func (b *backups) walgLoop(ctx context.Context) {
	ticker := time.NewTicker(walgTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		b.walgOnce(ctx)
	}
}

func (b *backups) walgOnce(ctx context.Context) {
	b.walg.run.Lock()
	defer b.walg.run.Unlock()
	if _, err := b.ready(); err != nil {
		b.walgStopAll(ctx, nil)
		return
	}
	wanted := map[string]bool{}
	for app, databases := range b.pgDatabases() {
		for _, database := range databases {
			wanted[companionName(app, database.Service)] = true
			if err := b.walgEnsure(ctx, app, database); err != nil {
				slog.Warn("backup: continuous Postgres backup", "app", app, "database", database.Path, "error", err)
				continue
			}
			if err := b.walgSwitch(ctx, app, database); err != nil {
				slog.Warn("backup: switch WAL", "app", app, "database", database.Path, "error", err)
			}
			state := b.walgState(drillKey(app, database))
			b.walg.mu.Lock()
			due := time.Since(state.based) > walgBaseEvery
			b.walg.mu.Unlock()
			if due {
				if err := b.walgBase(ctx, app, database); err != nil {
					slog.Error("backup: base backup failed", "app", app, "database", database.Path, "error", err)
				}
			}
		}
	}
	b.walgStopAll(ctx, wanted)
}

// walgStopAll removes the companions not in keep. Where their Postgres
// still runs, it drops the replication slot too: an unread slot keeps
// every WAL file and would fill the disk.
func (b *backups) walgStopAll(ctx context.Context, keep map[string]bool) {
	out, err := b.apps.output(ctx, "docker", "ps", "-a", "--filter", "label=ops.walg", "--format", `{{.Names}} {{.Label "ops.walg"}}`)
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, owner, _ := strings.Cut(line, " ")
		if name == "" || keep[name] {
			continue
		}
		_, _ = b.apps.output(ctx, "docker", "rm", "-f", name)
		app, service, _ := strings.Cut(owner, "/")
		target, err := b.apps.postgres(ctx, app, protocol.Database{Kind: protocol.DatabasePostgres, Service: service})
		if err != nil {
			continue
		}
		_, err = target.psql(ctx, `SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = :'slot' AND NOT active;`, map[string]string{"slot": walgSlot}, false)
		if err != nil {
			slog.Warn("backup: drop the WAL-G slot", "app", app, "service", service, "error", err)
		}
	}
}

// walgBackup is one finished base backup in the bucket.
type walgBackup struct {
	name string
	at   time.Time
}

// walgBaseBackups lists the finished base backups of a database, oldest
// first, from the stop sentinels WAL-G writes when one is done.
func (b *backups) walgBaseBackups(ctx context.Context, app string, database protocol.Database) ([]walgBackup, error) {
	storage, prefix, err := b.walgPrefix(app, database)
	if err != nil {
		return nil, err
	}
	client := s3Client(storage)
	folder := prefix + "/basebackups_005/"
	found := []walgBackup{}
	pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(storage.Bucket), Prefix: aws.String(folder), Delimiter: aws.String("/")})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			name, ok := strings.CutSuffix(strings.TrimPrefix(aws.ToString(object.Key), folder), "_backup_stop_sentinel.json")
			if ok && object.LastModified != nil {
				found = append(found, walgBackup{name: name, at: object.LastModified.UTC()})
			}
		}
	}
	slices.SortFunc(found, func(x, y walgBackup) int { return x.at.Compare(y.at) })
	return found, nil
}

// walgWindow is how far back a point in time restore reaches: from the
// oldest finished base backup to the newest WAL segment in the bucket.
func (b *backups) walgWindow(ctx context.Context, app string, database protocol.Database) (since, until time.Time, err error) {
	backups, err := b.walgBaseBackups(ctx, app, database)
	if err != nil || len(backups) == 0 {
		return since, until, err
	}
	since = backups[0].at
	until = backups[len(backups)-1].at
	storage, prefix, err := b.walgPrefix(app, database)
	if err != nil {
		return since, until, err
	}
	pages := s3.NewListObjectsV2Paginator(s3Client(storage), &s3.ListObjectsV2Input{Bucket: aws.String(storage.Bucket), Prefix: aws.String(prefix + "/wal_005/")})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return since, until, err
		}
		for _, object := range page.Contents {
			if object.LastModified != nil && object.LastModified.After(until) && !strings.HasSuffix(aws.ToString(object.Key), ".history.lz4") {
				until = object.LastModified.UTC()
			}
		}
	}
	return since, until, nil
}

// walgUnsupported says why a database has snapshots only, or "".
func (b *backups) walgUnsupported(app string, database protocol.Database) string {
	state := b.walgState(drillKey(app, database))
	b.walg.mu.Lock()
	defer b.walg.mu.Unlock()
	return state.unsupported
}

// walgBefore takes a base backup before a deploy, when the database has a
// companion running.
func (b *backups) walgBefore(ctx context.Context, app string, logf func(string, ...any)) error {
	if _, err := b.ready(); err != nil {
		return nil
	}
	for _, database := range b.apps.runningDatabases(app) {
		if !database.Postgres() {
			continue
		}
		running, _ := b.apps.output(ctx, "docker", "inspect", "-f", "{{.State.Running}}", companionName(app, database.Service))
		if strings.TrimSpace(running) != "true" {
			continue
		}
		if err := b.walgBase(ctx, app, database); err != nil {
			return fmt.Errorf("base backup of %s before the deploy: %w", database.Path, err)
		}
		if logf != nil {
			logf("base backup of %s", database.Path)
		}
	}
	return nil
}

// pgRestorePoint puts a Postgres database back as it was at a point in
// time. It takes a snapshot first and stops the app. Into a new volume it
// fetches the newest base backup that finished before the time and lets
// Postgres replay the WAL up to it. Only when that worked does the new data
// replace the old, and the app starts again.
func (b *backups) pgRestorePoint(ctx context.Context, request protocol.BackupRestore, database protocol.Database) error {
	at := request.At.UTC()
	backups, err := b.walgBaseBackups(ctx, request.App, database)
	if err != nil {
		return err
	}
	base := ""
	for _, backup := range backups {
		if !backup.at.After(at) {
			base = backup.name
		}
	}
	since, until, _ := b.walgWindow(ctx, request.App, database)
	if base == "" || at.After(until) {
		return fmt.Errorf("%s UTC is outside the point in time window of %s (%s to %s UTC): pick a time in it, or a snapshot",
			at.Format(time.DateTime), database.Path, since.Format(time.DateTime), until.Format(time.DateTime))
	}
	defer b.apps.lock(request.App)()
	target, err := b.apps.postgres(ctx, request.App, database)
	if err != nil {
		return err
	}
	data, err := b.apps.pgDataOf(ctx, target.container)
	if err != nil {
		return err
	}
	storage, prefix, err := b.walgPrefix(request.App, database)
	if err != nil {
		return err
	}
	binary, err := b.walgBinary(ctx)
	if err != nil {
		return err
	}
	if _, err := b.snapshotOne(ctx, request.App, database, "before-restore"); err != nil {
		return fmt.Errorf("snapshot before the restore: %w", err)
	}
	envFile, remove, err := walgEnv(storage, prefix, target, data)
	if err != nil {
		return err
	}
	defer remove()

	// Recover into a new volume while the app keeps running.
	volume := "ops-restore-" + request.App + "-" + database.Service + "-" + time.Now().UTC().Format("20060102t150405")
	if _, err := b.apps.output(ctx, "docker", "volume", "create", volume); err != nil {
		return err
	}
	defer func() { _, _ = b.apps.output(context.Background(), "docker", "volume", "rm", "-f", volume) }()
	run := func(user string, extra []string, command ...string) (string, error) {
		args := []string{"run", "--rm", "-u", user, "-v", volume + ":/restore"}
		args = append(args, walgRunArgs(binary, envFile)...)
		args = append(args, extra...)
		args = append(args, target.image)
		return b.apps.output(ctx, "docker", append(args, command...)...)
	}
	if out, err := run("root", nil, "sh", "-c", "chown postgres:postgres /restore && chmod 700 /restore"); err != nil {
		return fmt.Errorf("prepare the restore: %v %s", err, out)
	}
	if out, err := run("postgres", []string{"--entrypoint", "/usr/local/bin/wal-g"}, "backup-fetch", "/restore", base); err != nil {
		return fmt.Errorf("fetch the base backup %s: %v %s", base, err, out)
	}
	recover := "recover-" + volume
	args := []string{"run", "-d", "--name", recover, "-u", "postgres", "-v", volume + ":/restore", "-e", "PGDATA=/restore"}
	args = append(args, walgRunArgs(binary, envFile)...)
	args = append(args, "--entrypoint", "sh", target.image, "-c",
		`touch /restore/recovery.signal && exec postgres -D /restore -c listen_addresses='' -c restore_command='/usr/local/bin/wal-g wal-fetch %f %p' -c recovery_target_time="$0" -c recovery_target_action=promote`,
		at.Format("2006-01-02 15:04:05.000000+00"))
	if out, err := b.apps.output(ctx, "docker", args...); err != nil {
		return fmt.Errorf("start the recovery: %v %s", err, out)
	}
	defer func() { _, _ = b.apps.output(context.Background(), "docker", "rm", "-f", recover) }()
	recovering := pgTarget{container: recover, user: target.user, database: "postgres"}
	promoted := false
	for deadline := time.Now().Add(30 * time.Minute); time.Now().Before(deadline); {
		out, err := recovering.psql(ctx, "SELECT pg_is_in_recovery();", nil, false)
		if err == nil && strings.TrimSpace(out) == "f" {
			promoted = true
			break
		}
		state, _ := b.apps.output(ctx, "docker", "inspect", "-f", "{{.State.Running}}", recover)
		if strings.TrimSpace(state) == "false" {
			logs, _ := b.apps.output(ctx, "docker", "logs", "--tail", "5", recover)
			return fmt.Errorf("the recovery stopped, nothing changed: %s", strings.TrimSpace(logs))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if !promoted {
		return errors.New("the recovery took longer than 30 minutes, nothing changed")
	}
	if out, err := b.apps.output(ctx, "docker", "stop", "-t", "120", recover); err != nil {
		return fmt.Errorf("stop the recovery: %v %s", err, out)
	}

	// Swap: stop the app and its companion, replace the data, start again.
	composeFile := filepath.Join(b.apps.releaseDir(request.App, b.apps.current(request.App)), "compose.json")
	noLog := func(string, ...any) {}
	_, _ = b.apps.output(ctx, "docker", "rm", "-f", companionName(request.App, database.Service))
	if err := b.apps.compose(ctx, request.App, composeFile, noLog, "stop"); err != nil {
		return fmt.Errorf("stop the app: %w", err)
	}
	copyScript := `set -e; find "$0" -mindepth 1 -delete; cp -a /restore/. "$0"/; rm -f "$0"/recovery.signal`
	_, copyErr := b.apps.output(ctx, "docker", "run", "--rm", "-u", "root", "-v", volume+":/restore:ro", "-v", data.mount, "--entrypoint", "sh", target.image, "-c", copyScript, data.dir)
	startErr := b.apps.compose(ctx, request.App, composeFile, noLog, "start")
	if copyErr != nil {
		return fmt.Errorf("swap the data: %w; the snapshot before the restore has the old state", copyErr)
	}
	if startErr != nil {
		return fmt.Errorf("start the app again: %w", startErr)
	}
	// A new timeline starts: a base backup on it keeps the window whole.
	state := b.walgState(drillKey(request.App, database))
	b.walg.mu.Lock()
	state.lsn = ""
	b.walg.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		if err := b.walgEnsure(ctx, request.App, database); err == nil {
			if err := b.walgBase(ctx, request.App, database); err != nil {
				slog.Warn("backup: base backup after the restore", "app", request.App, "error", err)
			}
		}
	}()
	slog.Info("backup: restored to a point in time", "app", request.App, "database", database.Path, "at", at)
	return nil
}

// walgObjects counts what is under a database's WAL-G folder, for tests.
func (b *backups) walgObjects(ctx context.Context, app string, database protocol.Database, folder string) (int, error) {
	storage, prefix, err := b.walgPrefix(app, database)
	if err != nil {
		return 0, err
	}
	count := 0
	pages := s3.NewListObjectsV2Paginator(s3Client(storage), &s3.ListObjectsV2Input{Bucket: aws.String(storage.Bucket), Prefix: aws.String(path.Join(prefix, folder) + "/")})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return 0, err
		}
		count += len(page.Contents)
	}
	return count, nil
}
