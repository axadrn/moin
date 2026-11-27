package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"filippo.io/age"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// A backup is a copy of a database in the user's own bucket, taken here
// on the server on the app's schedule and kept as long as the app says;
// every day moin checks that the newest one restores. Data goes from the
// server to the bucket and nowhere else. One engine per kind of
// database, with the same verbs:
//
//	database ── Copy ───▶ bucket <database>/copies/<id>
//	copy ────── Check ──▶ a throwaway database, read through
//	copy ────── Restore ▶ built beside the live one, one switch,
//	                      the live one kept as the undo
//
// backup_sqlite.go copies with VACUUM INTO, backup_postgres.go with
// pg_dump.
type engine interface {
	// Copy writes a full copy of the live database to w.
	Copy(ctx context.Context, w io.Writer) error
	// Restore builds the database beside the live one, stops the app,
	// switches in one atomic step, the live one kept as the undo id, and
	// starts the app. There is never a moment without a live database.
	Restore(ctx context.Context, from source, id string) error
	// Recover tidies after a restore that was cut off and says whether it
	// had switched, from what is there.
	Recover(ctx context.Context, mark restoreMark) (switched bool, err error)
	// Check restores a copy into a throwaway database and reads it all.
	Check(ctx context.Context, from source) error
	// Size is how many bytes the live database takes.
	Size(ctx context.Context) (int64, error)
	// Undos are the states before restores, newest first; Drop removes one.
	Undos(ctx context.Context) ([]protocol.Copy, error)
	Drop(ctx context.Context, id string) error
}

// source is what a restore goes back to: a copy from the bucket or the
// undo of an earlier restore. size is how much room building it takes.
type source struct {
	copy io.Reader
	undo string
	size int64
}

const (
	day = 24 * time.Hour
	// stallLimit stops a copy when no data has moved for this long. A big
	// database may take hours, as long as it keeps moving. A copy that
	// hangs would hold up every other copy.
	stallLimit = 30 * time.Minute
	// sizeNote is the name of the note on each copy that says how many
	// bytes its database took. A restore needs that much room.
	sizeNote = "database-size"
)

// backups runs the schedule of every database on the server. The daemon
// has one for its timers; each `moin here …` over SSH has its own for one
// command. What they share is on the disk: the config, the state the
// daemon writes down (stateFile) and the lock of a restore.
type backups struct {
	apps *apps
	// load reads the server's config: the file, or what a test set.
	load func() (Config, error)
	// good is the last config without a mistake, mistake what is wrong in
	// the file now: a typo stops no backup.
	configMu sync.Mutex
	good     Config
	mistake  error

	mu     sync.Mutex
	states map[string]*state
	// ticking lets one round run at a time.
	ticking sync.Mutex
	// found are the databases of the last look and when it was (running).
	looking sync.Mutex
	found   map[string][]protocol.Database
	foundAt time.Time
	// other names another server, whose copies in the bucket are read
	// instead of this one's: a restore of what a server that is gone copied.
	other string
	// key opens the copies instead of the server's own key, when a restore
	// brought yours.
	key age.Identity
}

// state is what the daemon knows of one database: its newest copy, why
// the last one failed and since when, the newest daily check, and the
// copy the last check that worked restored. BucketDown says the copy
// failed at the bucket, not at the database.
type state struct {
	LastCopy     time.Time `json:"last_copy,omitzero"`
	CopyError    string    `json:"copy_error,omitempty"`
	BucketDown   bool      `json:"bucket_down,omitempty"`
	FailingSince time.Time `json:"failing_since,omitzero"`
	CheckedAt    time.Time `json:"checked_at,omitzero"`
	CheckError   string    `json:"check_error,omitempty"`
	CheckedCopy  string    `json:"checked_copy,omitempty"`
}

func newBackups(a *apps) *backups {
	b := &backups{apps: a, states: map[string]*state{}, load: LoadConfig}
	b.loadStates()
	return b
}

// configNow is the server's config, and what is wrong in its file. A file
// with a mistake changes nothing: the last good config keeps working.
func (b *backups) configNow() (Config, error) {
	config, err := b.load()
	b.configMu.Lock()
	defer b.configMu.Unlock()
	if b.mistake = err; err == nil {
		b.good = config
	}
	return b.good, b.mistake
}

// config is the server's config, the last good one (configNow).
func (b *backups) config() Config {
	config, _ := b.configNow()
	return config
}

// stateFile is where the daemon writes down what it knows of each
// database, so a restart forgets no check and `moin backups` can show it.
func (b *backups) stateFile() string {
	return filepath.Join(filepath.Dir(b.apps.dir), "backups.json")
}

func (b *backups) loadStates() {
	b.mu.Lock()
	defer b.mu.Unlock()
	body, _ := os.ReadFile(b.stateFile())
	states := map[string]*state{}
	if json.Unmarshal(body, &states) == nil {
		b.states = states
	}
}

// saveStates writes the states down. Only the daemon does.
func (b *backups) saveStates() {
	b.mu.Lock()
	defer b.mu.Unlock()
	body, _ := json.Marshal(b.states)
	if err := writeWhole(b.stateFile(), body); err != nil {
		slog.Error("backup: write down the state", "error", err)
	}
}

func (b *backups) engine(app string, database protocol.Database) engine {
	if database.Postgres() {
		return &postgresEngine{b: b, app: app, database: database}
	}
	return &sqliteEngine{b: b, app: app, database: database}
}

// state is the state of a database, by its file or its app and service.
// The caller holds the lock.
func (b *backups) state(app string, database protocol.Database) *state {
	k := database.HostPath
	if database.Postgres() {
		k = app + "/" + database.Service + "/" + database.Name
	}
	if b.states[k] == nil {
		b.states[k] = &state{}
	}
	return b.states[k]
}

// configure makes the backups take config instead of the file.
func (b *backups) configure(config Config) {
	b.load = func() (Config, error) { return config, nil }
}

// on says the server has a bucket.
func (b *backups) on() bool { return b.config().Bucket != nil }

// errNoBucket is why nothing is copied on a server without a bucket.
var errNoBucket = errors.New("no bucket yet: set one with `" + build.Name + " bucket <server> set`")

// where is the bucket of a database and its folder in it.
func (b *backups) where(app string, database protocol.Database) (*Bucket, string, error) {
	config, err := b.configNow()
	if config.Bucket == nil {
		// A config that never read well may have a bucket nobody knows of.
		return nil, "", cmp.Or(err, errNoBucket)
	}
	if b.other != "" {
		config.Name = b.other
	}
	return config.Bucket, databaseKey(config, app, database.Path), nil
}

// databaseKey is the folder of a database in the bucket:
// <path>/<server>/<app>/<database>, the database as its path in the
// container with ~ for /, like data~app.db, postgres-db or
// postgres-db~analytics.
func databaseKey(config Config, app, database string) string {
	// Each / becomes ~, so /data/app.db and /data-app.db never share one.
	name := strings.NewReplacer("%", "%25", "~", "%7E", "/", "~").Replace(strings.Trim(database, "/"))
	return strings.Trim(path.Join(config.Bucket.Path, config.Name, app, name), "/")
}

// copyKey is where a copy lives in its bucket.
func copyKey(folder string, copy protocol.Copy, database protocol.Database) *string {
	return aws.String(folder + "/" + protocol.CopyFile(copy.ID, database.Postgres(), copy.Plain))
}

// copyOf is the copy a key in the bucket holds: its ID, and whether it is
// a plain one, from before copies were encrypted.
func copyOf(key string, database protocol.Database) protocol.Copy {
	name := path.Base(key)
	plain := path.Base(protocol.CopyFile("", database.Postgres(), true))
	if id, ok := strings.CutSuffix(name, plain+protocol.Encrypted); ok {
		return protocol.Copy{ID: id}
	}
	return protocol.Copy{ID: strings.TrimSuffix(name, plain), Plain: true}
}

// bucketError is a failure of the bucket, not of a database: one problem
// for the server, not one for each database.
type bucketError struct{ err error }

func (e bucketError) Error() string { return "the bucket: " + shortS3(e.err) }
func (e bucketError) Unwrap() error { return e.err }

// shortS3 is what a bucket said, short: "AccessDenied: Access Denied", or
// why it was not reached. The SDK's own line names the operation, the
// request and its retries, which helps nobody in an alert.
func shortS3(err error) string {
	var said smithy.APIError
	if errors.As(err, &said) {
		return said.ErrorCode() + ": " + said.ErrorMessage()
	}
	var unreached *url.Error
	if errors.As(err, &unreached) {
		return unreached.Err.Error()
	}
	return err.Error()
}

// remember is a reader that keeps the error its reading failed with.
type remember struct {
	reader io.Reader
	failed error
}

func (r *remember) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err != nil && err != io.EOF {
		r.failed = err
	}
	return n, err
}

// copy streams a copy of a database into the bucket as id, encrypted on
// the way (encrypt).
func (b *backups) copy(ctx context.Context, app string, database protocol.Database, id string) (string, error) {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return "", err
	}
	at, _ := protocol.CopyTime(id)
	engine := b.engine(app, database)
	size, err := engine.Size(ctx)
	if err != nil {
		return "", fmt.Errorf("copy %s: %w", database.Path, err)
	}
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	reader, writer := io.Pipe()
	go func() { writer.CloseWithError(b.copyEncrypted(ctx, engine, writer)) }()
	// What the reading fails with is the database's own failure.
	fromDatabase := &remember{reader: reader}
	body, stopWatching := stopWhenStuck(fromDatabase, firstDataLimit(size), stallLimit, stop)
	defer stopWatching()
	// Parts of 32 MB, two at a time: up to 320 GB in 64 MB of memory.
	uploader := manager.NewUploader(s3Client(bucket), func(u *manager.Uploader) { u.PartSize, u.Concurrency = 32<<20, 2 })
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(bucket.Bucket),
		Key:      copyKey(folder, protocol.Copy{ID: id}, database),
		Body:     body,
		Metadata: map[string]string{sizeNote: strconv.FormatInt(size, 10)},
	})
	// The upload is over: this frees a writer that still waits.
	reader.CloseWithError(errors.New("the upload ended"))
	switch stuck := context.Cause(ctx); {
	case err == nil:
	case stuck != nil:
		err = stuck
	case fromDatabase.failed != nil:
		err = fromDatabase.failed
	default:
		err = bucketError{err}
	}
	if err != nil {
		return "", fmt.Errorf("copy %s: %w", database.Path, err)
	}
	b.mu.Lock()
	if s := b.state(app, database); at.After(s.LastCopy) {
		s.LastCopy = at
	}
	b.mu.Unlock()
	return id, nil
}

// copyEncrypted writes an encrypted copy of the engine's database to w.
func (b *backups) copyEncrypted(ctx context.Context, engine engine, w io.Writer) error {
	encrypted, err := b.encrypt(w)
	if err != nil {
		return err
	}
	if err := engine.Copy(ctx, encrypted); err != nil {
		return err
	}
	return encrypted.Close()
}

// watchedReader is a reader that restarts a timer each time data comes.
type watchedReader struct {
	reader io.Reader
	timer  *time.Timer
	limit  time.Duration
}

func (w watchedReader) Read(p []byte) (int, error) {
	n, err := w.reader.Read(p)
	if n > 0 {
		w.timer.Reset(w.limit)
	}
	return n, err
}

// firstDataLimit is how long a copy may take to send its first byte.
// SQLite writes its whole clean copy first, which takes longer the bigger
// the database is: we allow 10 MB a second on top of stallLimit.
func firstDataLimit(size int64) time.Duration {
	return stallLimit + time.Duration(size/(10<<20))*time.Second
}

// stopWhenStuck calls stop when no data has come from reader: first for
// the first byte, then limit between one byte and the next. The second
// result ends the watching.
func stopWhenStuck(reader io.Reader, first, limit time.Duration, stop context.CancelCauseFunc) (io.Reader, func()) {
	timer := time.AfterFunc(first, func() {
		stop(errors.New("no data moved for too long"))
	})
	return watchedReader{reader: reader, timer: timer, limit: limit}, func() { timer.Stop() }
}

// listWait is how long a bucket gets to say which copies it has. One
// that is there answers in a moment.
const listWait = 15 * time.Second

// copies are the copies of a database in the bucket, newest first.
func (b *backups) copies(ctx context.Context, app string, database protocol.Database) ([]protocol.Copy, error) {
	ctx, cancel := context.WithTimeout(ctx, listWait)
	defer cancel()
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return nil, err
	}
	copies := []protocol.Copy{}
	pages := s3.NewListObjectsV2Paginator(s3Client(bucket), &s3.ListObjectsV2Input{Bucket: aws.String(bucket.Bucket), Prefix: aws.String(folder + "/copies/")})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, bucketError{err}
		}
		for _, object := range page.Contents {
			copy := copyOf(aws.ToString(object.Key), database)
			if at, ok := protocol.CopyTime(copy.ID); ok {
				copy.At, copy.Size = at, aws.ToInt64(object.Size)
				copies = append(copies, copy)
			}
		}
	}
	slices.SortFunc(copies, func(x, y protocol.Copy) int { return y.At.Compare(x.At) })
	return copies, nil
}

// errNoCopy is a copy that is not in the bucket.
var errNoCopy = errors.New("is not in the bucket")

// readCopy opens a copy in the bucket as it lies there, encrypted, and
// says how many bytes its database took. The caller closes it.
func (b *backups) readCopy(ctx context.Context, app string, database protocol.Database, copy protocol.Copy) (io.ReadCloser, int64, error) {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return nil, 0, err
	}
	object, err := s3Client(bucket).GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket.Bucket), Key: copyKey(folder, copy, database)})
	var gone *types.NoSuchKey
	if errors.As(err, &gone) {
		return nil, 0, fmt.Errorf("the copy %s of %s %w", copy.ID, database.Path, errNoCopy)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("the copy %s of %s: %w", copy.ID, database.Path, bucketError{err})
	}
	// No note means no number: only the spare room is checked.
	size, _ := strconv.ParseInt(object.Metadata[sizeNote], 10, 64)
	return object.Body, size, nil
}

// deleteCopy deletes a copy in the bucket.
func (b *backups) deleteCopy(ctx context.Context, app string, database protocol.Database, copy protocol.Copy) error {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return err
	}
	_, err = s3Client(bucket).DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: copyKey(folder, copy, database)})
	return err
}

// keepNewest is how many of the newest copies always stay, however old.
// A clock that jumped ahead for one copy takes no real copy with it.
const keepNewest = 3

// prune deletes the copies and undos older than the app keeps them,
// counted from the newest copy. The newest copies (keepNewest) and the
// copy the last check restored always stay.
func (b *backups) prune(ctx context.Context, app string, database protocol.Database, copies []protocol.Copy) error {
	if len(copies) == 0 {
		return nil
	}
	_, keep := b.config().schedule(app)
	before := copies[0].At.Add(-keep)
	b.mu.Lock()
	proven := b.state(app, database).CheckedCopy
	b.mu.Unlock()
	var err error
	for index, copy := range copies {
		if index >= keepNewest && copy.ID != proven && copy.At.Before(before) {
			err = errors.Join(err, b.deleteCopy(ctx, app, database, copy))
		}
	}
	e := b.engine(app, database)
	undos, undosErr := e.Undos(ctx)
	for _, undo := range undos {
		if undo.At.Before(before) {
			err = errors.Join(err, e.Drop(ctx, undo.ID))
		}
	}
	return errors.Join(err, undosErr)
}

// loop starts the work of the backups: every minute the copies and checks
// that are due, and every hour the search for new databases. Each runs on
// its own, so a copy that takes hours holds up nothing else.
func (b *backups) loop(ctx context.Context) {
	minute := time.NewTicker(time.Minute)
	defer minute.Stop()
	hourly := time.NewTimer(time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-minute.C:
			go b.tick(ctx)
		case <-hourly.C:
			go b.searchDatabases(ctx)
			hourly.Reset(time.Hour)
		}
	}
}

// tick ends the restores a stopped command left, takes the copies and
// runs the checks that are due, and writes down how it went. The restores
// come first and wait for no copy: their app is stopped.
func (b *backups) tick(ctx context.Context) {
	b.recover(ctx)
	if !b.ticking.TryLock() {
		return
	}
	defer b.ticking.Unlock()
	if !b.on() {
		return
	}
	for app, databases := range b.running(ctx) {
		for _, database := range databases {
			b.copyIfDue(ctx, app, database)
			b.checkIfDue(ctx, app, database)
			b.saveStates()
		}
	}
}

// copyIfDue takes the copy of one database when its time came, and notes
// how it went. While it runs it shares the app's restore lock, so a
// restore waits for it; during a restore it does nothing.
func (b *backups) copyIfDue(ctx context.Context, app string, database protocol.Database) {
	unlock, ok := tryLock(b.restoreLock(app), syscall.LOCK_SH)
	if !ok {
		return
	}
	defer unlock()
	// A restore that was cut off ends first (recover).
	if b.restoring(app) {
		return
	}
	b.mu.Lock()
	copied := b.state(app, database).LastCopy
	b.mu.Unlock()
	due, _ := b.config().schedule(app)
	// A copy from the future, after the clock jumped, is no reason to wait.
	if time.Since(copied) < due && !copied.After(time.Now()) {
		return
	}
	err := b.copyDue(ctx, app, database, due)
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state(app, database)
	if err == nil {
		s.CopyError, s.BucketDown, s.FailingSince = "", false, time.Time{}
		return
	}
	slog.Error("backup: the copy failed", "app", app, "database", database.Path, "error", err)
	if s.CopyError == "" {
		s.FailingSince = time.Now().UTC()
	}
	var down bucketError
	s.CopyError, s.BucketDown = err.Error(), errors.As(err, &down)
}

// checkAgain is how long after a check that failed the next one runs: a
// bucket that was away, or a newer copy, may prove the backup again.
const checkAgain = time.Hour

// checkIfDue runs the daily check of one database: a day after the last
// one, or checkAgain after one that failed.
func (b *backups) checkIfDue(ctx context.Context, app string, database protocol.Database) {
	b.mu.Lock()
	s := *b.state(app, database)
	b.mu.Unlock()
	since := time.Since(s.CheckedAt)
	if since >= day || s.CheckError != "" && since >= checkAgain {
		b.check(ctx, app, database)
	}
}

// searchDatabases looks for new databases in every app.
func (b *backups) searchDatabases(ctx context.Context) {
	for _, app := range b.apps.names() {
		b.apps.findNewDatabases(ctx, app)
	}
}

// copyDue takes a copy when the newest in the bucket is older than due,
// and deletes the old ones.
func (b *backups) copyDue(ctx context.Context, app string, database protocol.Database, due time.Duration) error {
	copies, err := b.copies(ctx, app, database)
	if err != nil {
		return fmt.Errorf("copy %s: %w", database.Path, err)
	}
	// Copies from the future, after the clock jumped, do not count.
	for len(copies) > 0 && copies[0].At.After(time.Now()) {
		copies = copies[1:]
	}
	if len(copies) > 0 && time.Since(copies[0].At) < due {
		b.mu.Lock()
		b.state(app, database).LastCopy = copies[0].At
		b.mu.Unlock()
		return nil
	}
	at := time.Now().UTC().Truncate(time.Second)
	id, err := b.copy(ctx, app, database, protocol.NewCopyID(at))
	if err != nil {
		return err
	}
	return b.prune(ctx, app, database, append([]protocol.Copy{{ID: id, At: at}}, copies...))
}

// cannotBackUp are the statuses of what looks like a database of the app
// but moin cannot back up. Every surface shows them as a problem.
func (b *backups) cannotBackUp(app string) []protocol.DatabaseBackup {
	statuses := []protocol.DatabaseBackup{}
	detected, _ := b.apps.live(app)
	for _, database := range detected.NoBackup {
		statuses = append(statuses, protocol.DatabaseBackup{Database: database.Path, Kind: database.Kind, Error: noBackupText})
	}
	return statuses
}

// failGrace is how long copies may fail before it is a problem: a bucket
// that is away for a moment is none.
const failGrace = 10 * time.Minute

// status is how one database is backed up; full also reads the copies
// and the undos.
func (b *backups) status(ctx context.Context, app string, database protocol.Database, full bool) protocol.DatabaseBackup {
	status := protocol.DatabaseBackup{Database: database.Path, Kind: database.Kind}
	b.mu.Lock()
	s := *b.state(app, database)
	b.mu.Unlock()
	if !s.LastCopy.IsZero() {
		status.CopiedAt = &s.LastCopy
	}
	if !s.CheckedAt.IsZero() {
		status.CheckedAt, status.CheckError, status.CheckedCopy = &s.CheckedAt, s.CheckError, s.CheckedCopy
	}
	switch {
	case !b.on():
		status.Error = errNoBucket.Error()
	case s.CopyError != "" && time.Since(s.FailingSince) > failGrace:
		status.Error, status.BucketDown = s.CopyError, s.BucketDown
	// Copies that stopped coming are a problem too, with an alert.
	case b.late(app, s.LastCopy):
		status.Error = "the copies are late: the newest is from " + s.LastCopy.UTC().Format("Jan 2 15:04 UTC")
	}
	if full && b.on() {
		copies, err := b.copies(ctx, app, database)
		if err != nil {
			status.Error = err.Error()
		}
		if status.Copies = copies; len(copies) > 0 {
			status.CopiedAt = &copies[0].At
		}
		status.Undos, _ = b.engine(app, database).Undos(ctx)
	}
	return status
}

// late says the newest copy is older than twice how often the app is
// copied, and than one more copy may take.
func (b *backups) late(app string, copied time.Time) bool {
	if copied.IsZero() {
		return false
	}
	every, _ := b.config().schedule(app)
	return time.Since(copied) > max(2*every, every+time.Hour)
}

// statuses are how the databases of every app stand, by app, from what
// the daemon knows: nothing is read from the bucket.
func (b *backups) statuses(ctx context.Context) map[string][]protocol.DatabaseBackup {
	all := map[string][]protocol.DatabaseBackup{}
	for app, databases := range b.running(ctx) {
		for _, database := range databases {
			all[app] = append(all[app], b.status(ctx, app, database, false))
		}
		all[app] = append(all[app], b.cannotBackUp(app)...)
	}
	return all
}

// check restores the newest copy of one database into a throwaway one,
// reads it and notes how it went. Without a copy there is nothing to
// check, and a bucket that is away already shows as its own problem. A
// copy whose reading broke is read once more.
func (b *backups) check(ctx context.Context, app string, database protocol.Database) {
	copies, err := b.copies(ctx, app, database)
	if err != nil || len(copies) == 0 {
		return
	}
	newest := copies[0]
	cut, err := b.checkOnce(ctx, app, database, newest)
	if cut {
		slog.Warn("backup: reading the copy for the daily check broke, once more", "app", app, "database", database.Path, "copy", newest.ID, "error", err)
		cut, err = b.checkOnce(ctx, app, database, newest)
	}
	if errors.Is(err, errNoCopy) {
		// A newer copy came and the old one went meanwhile.
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state(app, database)
	s.CheckedAt, s.CheckError = time.Now().UTC(), ""
	switch {
	case err == nil:
		s.CheckedCopy = newest.ID
		slog.Info("backup: the daily check restored the newest copy", "app", app, "database", database.Path, "copy", newest.ID)
		return
	case cut:
		s.CheckError = "could not read the copy " + newest.ID + ": " + shortS3(err)
	default:
		s.CheckError = "the copy " + newest.ID + " did not restore: " + err.Error()
	}
	slog.Error("backup: the daily check failed", "app", app, "database", database.Path, "copy", newest.ID, "error", err)
}

// checkOnce restores one copy into a throwaway database and reads it. cut
// says the reading from the bucket broke or stopped moving, which says
// nothing about the copy.
func (b *backups) checkOnce(ctx context.Context, app string, database protocol.Database, copy protocol.Copy) (cut bool, err error) {
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	body, size, err := b.readCopy(ctx, app, database, copy)
	if err != nil {
		return !errors.Is(err, errNoCopy), err
	}
	defer body.Close()
	fromBucket := &remember{reader: body}
	watched, stopWatching := stopWhenStuck(fromBucket, stallLimit, stallLimit, stop)
	defer stopWatching()
	// After the last byte the bucket is done: what follows is work here.
	plain, err := b.decrypt(copy, &untilEnd{reader: watched, done: stopWatching})
	if err == nil {
		err = b.engine(app, database).Check(ctx, source{copy: plain, size: size})
	}
	if stuck := context.Cause(ctx); stuck != nil {
		return true, stuck
	}
	if fromBucket.failed != nil {
		return true, fromBucket.failed
	}
	return false, err
}

// untilEnd is a reader that calls done when it has nothing more.
type untilEnd struct {
	reader io.Reader
	done   func()
}

func (u *untilEnd) Read(p []byte) (int, error) {
	n, err := u.reader.Read(p)
	if err != nil {
		u.done()
	}
	return n, err
}

// copyBefore copies the app's databases just before a deploy changes
// them, and names the copies. The first that fails ends it.
func (b *backups) copyBefore(ctx context.Context, app string) ([]string, error) {
	if b == nil || !b.on() {
		return nil, nil
	}
	var ids []string
	for _, database := range b.databases(ctx, app) {
		id, err := b.copy(ctx, app, database, protocol.NewCopyID(time.Now()))
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// runningFresh is how long one look at the databases counts.
const runningFresh = 50 * time.Second

// running are the databases of every app's live deploy. Looking asks
// every Postgres, so one look serves the copies and the report of the
// same minute.
func (b *backups) running(ctx context.Context) map[string][]protocol.Database {
	b.looking.Lock()
	defer b.looking.Unlock()
	if b.found != nil && time.Since(b.foundAt) < runningFresh {
		return b.found
	}
	found := map[string][]protocol.Database{}
	for _, app := range b.apps.names() {
		databases := b.databases(ctx, app)
		if len(databases) > 0 || len(b.cannotBackUp(app)) > 0 {
			found[app] = databases
		}
	}
	// A look that was cut off saw too little to keep.
	if ctx.Err() == nil {
		b.found, b.foundAt = found, time.Now()
	}
	return found
}

// one is the database a request names, or the app's only one.
func (b *backups) one(app, name string) (protocol.Database, error) {
	all := b.apps.liveDatabases(app)
	for _, database := range all {
		if database.Path == name || name == "" && len(all) == 1 {
			return database, nil
		}
		// Another database of a Postgres service: postgres-db/<name>.
		if other, ok := strings.CutPrefix(name, database.Path+"/"); ok && database.Postgres() && other != "" {
			database.Path, database.Name = name, other
			return database, nil
		}
	}
	if name == "" && len(all) > 1 {
		return protocol.Database{}, errors.New("the app has several databases: name the one")
	}
	return protocol.Database{}, fmt.Errorf("no database %s in %s", name, app)
}

// restoreMark is a restore in restores/<id>.json in the daemon's folder,
// written whole before anything changes, so a start after a crash knows
// it ran. Undo names the state before, which it leaves; From the undo it
// goes back to, if it does.
type restoreMark struct {
	ID         string     `json:"id"`
	App        string     `json:"app"`
	Database   string     `json:"database"`
	Undo       string     `json:"undo"`
	From       string     `json:"from,omitempty"`
	State      string     `json:"state"`
	Error      string     `json:"error,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

var restoreID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

func (b *backups) markFile(id string) string {
	return filepath.Join(filepath.Dir(b.apps.dir), "restores", id+".json")
}

// restoreLock is the file a restore of app holds locked alone while it
// runs, and every copy of the app shares while it runs: a restore waits
// for the copies, and no copy starts during a restore. A restore whose
// command stopped is one that is written down as running without the lock.
func (b *backups) restoreLock(app string) string {
	return filepath.Join(filepath.Dir(b.apps.dir), "locks", app+".restore")
}

// restore puts a database back and names the undo: the state before, to
// go back to. It is written down before anything changes and ends with a
// result either way. It waits for a copy or another restore of the app,
// and says so to out.
func (b *backups) restore(ctx context.Context, request protocol.BackupRestore, out io.Writer) (undo string, err error) {
	if !restoreID.MatchString(request.ID) {
		return "", errors.New("a restore needs an ID")
	}
	database, err := b.one(request.App, request.Database)
	if err != nil {
		return "", err
	}
	// Copies wait from here on.
	unlockRestore, err := b.lockRestore(ctx, request.App, out)
	if err != nil {
		return "", err
	}
	defer unlockRestore()
	// Nothing is written down for a copy or an undo that is not there.
	copy, err := b.restoreFrom(ctx, request, database)
	if err != nil {
		return "", err
	}
	mark := restoreMark{ID: request.ID, App: request.App, Database: database.Path, Undo: protocol.NewCopyID(time.Now()), From: request.Undo, State: protocol.RestoreRunning}
	if err := writeState(b.markFile(mark.ID), mark); err != nil {
		return "", fmt.Errorf("write the restore down: %w", err)
	}
	// A deploy that holds the lock ends first.
	unlock := b.apps.lock(request.App)
	from := source{undo: request.Undo}
	if request.Undo == "" {
		var body io.ReadCloser
		if body, from.size, err = b.readCopy(ctx, request.App, database, copy); err == nil {
			defer body.Close()
			from.copy, err = b.decrypt(copy, body)
		}
	}
	if err == nil {
		err = b.engine(request.App, database).Restore(ctx, from, mark.Undo)
	}
	unlock()
	b.finish(mark, err)
	return mark.Undo, err
}

// lockRestore takes the app's restore lock alone, once the copy or the
// restore that holds it ended.
func (b *backups) lockRestore(ctx context.Context, app string, out io.Writer) (unlock func(), err error) {
	for said := false; ; {
		if unlock, ok := tryLock(b.restoreLock(app), syscall.LOCK_EX); ok {
			return unlock, nil
		}
		if !said {
			fmt.Fprintf(out, "waiting for a copy or another restore of %s to end\n", app)
			said = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// restoreFrom checks that what a restore goes back to is there: the copy
// in the bucket, which it returns, or the undo on the server.
func (b *backups) restoreFrom(ctx context.Context, request protocol.BackupRestore, database protocol.Database) (protocol.Copy, error) {
	hint := fmt.Sprintf("See them with `%s backups %s`", build.Name, request.App)
	switch {
	case protocol.ValidCopyID(request.Copy):
		copies, err := b.copies(ctx, request.App, database)
		if err != nil {
			return protocol.Copy{}, err
		}
		for _, copy := range copies {
			if copy.ID == request.Copy {
				return copy, nil
			}
		}
		return protocol.Copy{}, fmt.Errorf("%s has no copy %s. %s", database.Path, request.Copy, hint)
	case protocol.ValidCopyID(request.Undo):
		undos, err := b.engine(request.App, database).Undos(ctx)
		if err != nil {
			return protocol.Copy{}, err
		}
		for _, undo := range undos {
			if undo.ID == request.Undo {
				return protocol.Copy{}, nil
			}
		}
		return protocol.Copy{}, fmt.Errorf("%s has no undo %s%s. %s", database.Path, undoPrefix, request.Undo, hint)
	}
	return protocol.Copy{}, errors.New("pick a copy or an undo to go back to. " + hint)
}

// finish writes down how a restore ended.
func (b *backups) finish(mark restoreMark, err error) {
	now := time.Now().UTC()
	mark.State, mark.FinishedAt = protocol.RestoreDone, &now
	if err != nil {
		mark.State, mark.Error = protocol.RestoreFailed, err.Error()
	}
	if err := writeState(b.markFile(mark.ID), mark); err != nil {
		slog.Error("backup: write down how a restore ended", "id", mark.ID, "error", err)
	}
	slog.Info("backup: restore", "app", mark.App, "database", mark.Database, "state", mark.State, "error", mark.Error)
}

// marks are the restores written down; one week of them.
func (b *backups) marks() []restoreMark {
	files, _ := filepath.Glob(b.markFile("*"))
	marks := []restoreMark{}
	for _, file := range files {
		var mark restoreMark
		if body, err := os.ReadFile(file); err != nil || json.Unmarshal(body, &mark) != nil {
			continue
		}
		if mark.FinishedAt != nil && time.Since(*mark.FinishedAt) > 7*day {
			os.Remove(file)
			continue
		}
		marks = append(marks, mark)
	}
	return marks
}

// restoring says a restore of app is written down as running: one that
// runs now, or one that was cut off and recover has to end first.
func (b *backups) restoring(app string) bool {
	return slices.ContainsFunc(b.marks(), func(mark restoreMark) bool {
		return mark.App == app && mark.State == protocol.RestoreRunning
	})
}

// recover ends the restores a stopped command left running, like one whose
// SSH connection broke: the database is whole either way, the app starts
// again, and the result says which database it runs on. When it cannot
// tell, the app starts all the same, and the mark stays for the next
// round to tell. A restore that still runs holds its lock and is left
// alone. The daemon runs it when it starts and every minute.
func (b *backups) recover(ctx context.Context) {
	for _, mark := range b.marks() {
		if mark.State != protocol.RestoreRunning {
			continue
		}
		if unlock, ok := tryLock(b.restoreLock(mark.App), syscall.LOCK_EX); ok {
			b.recoverRestore(ctx, mark)
			unlock()
		}
	}
}

// recoverRestore ends one restore that was cut off.
func (b *backups) recoverRestore(ctx context.Context, mark restoreMark) {
	database, err := b.one(mark.App, mark.Database)
	switched := false
	if err == nil {
		switched, err = b.engine(mark.App, database).Recover(ctx, mark)
	}
	// The switch is one step: whatever is live is whole, so the app
	// starts either way.
	startErr := b.apps.compose(context.WithoutCancel(ctx), mark.App, b.apps.composeJSON(mark.App), func(string, ...any) {}, "start")
	if err != nil {
		slog.Error("backup: how a restore that was cut off ended is not known yet", "app", mark.App, "database", mark.Database, "error", err)
		return
	}
	switch {
	case !switched:
		err = errors.New("the restore was cut off before the switch; the data is as it was before")
	case startErr != nil:
		err = fmt.Errorf("the restored data is in place, but start the app: %w", startErr)
	}
	b.finish(mark, err)
}

// removeOldTemp removes what copies and checks that were cut off left in
// the daemon's own folder, once it is a day old: nothing takes that long.
func (a *apps) removeOldTemp() {
	entries, _ := os.ReadDir(a.tempDir())
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > day {
			_ = os.RemoveAll(filepath.Join(a.tempDir(), entry.Name()))
		}
	}
}

// fileLock locks the file name until the result is called, across every
// moin process on the server: the daemon and each command over SSH. A
// process that dies lets go of it. It waits for whoever holds it.
func fileLock(name string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return func() { file.Close() }, nil
}

// tryFileLock is fileLock that does not wait: ok is false while another
// holds the lock.
func tryFileLock(name string) (unlock func(), ok bool) {
	return tryLock(name, syscall.LOCK_EX)
}

// tryLock locks the file name without waiting: alone (LOCK_EX), or
// shared with others that share it (LOCK_SH). ok is false while it
// cannot be had.
func tryLock(name string, how int) (unlock func(), ok bool) {
	if os.MkdirAll(filepath.Dir(name), 0o700) != nil {
		return nil, false
	}
	file, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB) != nil {
		file.Close()
		return nil, false
	}
	return func() { file.Close() }, true
}

// writeState writes value as JSON, whole (writeWhole).
func writeState(name string, value any) error {
	body, _ := json.Marshal(value)
	return writeWhole(name, body)
}

// writeWhole writes a file whole, 0600: a temp file, synced, renamed over
// the old one, the folder synced. A crash leaves the old file or the new
// one.
func writeWhole(name string, body []byte) error {
	temp := name + ".new"
	err := errors.Join(os.MkdirAll(filepath.Dir(name), 0o700), os.WriteFile(temp, body, 0o600))
	if err == nil {
		err = syncFile(temp)
	}
	if err == nil {
		err = os.Rename(temp, name)
	}
	if err == nil {
		err = syncFile(filepath.Dir(name))
	}
	return err
}

// syncFile writes a file, or a folder, to the disk.
func syncFile(name string) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}
