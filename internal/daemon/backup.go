package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

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
	// Recover tidies after a restore the daemon stopped in and says
	// whether it had switched, from what is there.
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
	// config reads the server's config: the file, or what a test set.
	config func() Config

	mu     sync.Mutex
	states map[string]*state
	// before is when the newest copy before a change of each app was.
	before map[string]time.Time
	// ticking lets one round run at a time.
	ticking sync.Mutex
}

// state is what the daemon knows of one database: its newest copy, why
// the last one failed and since when, the newest daily check, and the
// copy the last check that worked restored.
type state struct {
	LastCopy     time.Time `json:"last_copy"`
	CopyError    string    `json:"copy_error,omitempty"`
	FailingSince time.Time `json:"failing_since"`
	CheckedAt    time.Time `json:"checked_at"`
	CheckError   string    `json:"check_error,omitempty"`
	CheckedCopy  string    `json:"checked_copy,omitempty"`
}

func newBackups(a *apps) *backups {
	b := &backups{apps: a, states: map[string]*state{}, before: map[string]time.Time{}}
	b.config = func() Config {
		config, err := LoadConfig()
		if err != nil {
			slog.Error("backup: the config has a mistake, so nothing is copied", "error", err)
		}
		return config
	}
	b.loadStates()
	return b
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
	body, _ := json.Marshal(b.states)
	b.mu.Unlock()
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
	b.config = func() Config { return config }
}

// on says the server has a bucket.
func (b *backups) on() bool { return b.config().Bucket != nil }

// errNoBucket is why nothing is copied on a server without a bucket.
var errNoBucket = errors.New("no bucket yet: set one with `" + build.Name + " bucket <server> set`")

// where is the bucket of a database and its folder in it.
func (b *backups) where(app string, database protocol.Database) (*Bucket, string, error) {
	config := b.config()
	if config.Bucket == nil {
		return nil, "", errNoBucket
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
func copyKey(folder, id string, database protocol.Database) *string {
	return aws.String(folder + "/" + protocol.CopyFile(id, database.Postgres()))
}

// copyIDOf is the ID of a copy, from its key in the bucket.
func copyIDOf(key string, database protocol.Database) string {
	ending := path.Base(protocol.CopyFile("", database.Postgres()))
	return strings.TrimSuffix(path.Base(key), ending)
}

// copy streams a copy of a database into the bucket as id.
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
	go func() { writer.CloseWithError(engine.Copy(ctx, writer)) }()
	body, stopWatching := stopWhenStuck(reader, firstDataLimit(size), stallLimit, stop)
	defer stopWatching()
	// Parts of 32 MB, two at a time: up to 320 GB in 64 MB of memory.
	uploader := manager.NewUploader(s3Client(bucket), func(u *manager.Uploader) { u.PartSize, u.Concurrency = 32<<20, 2 })
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(bucket.Bucket),
		Key:      copyKey(folder, id, database),
		Body:     body,
		Metadata: map[string]string{sizeNote: strconv.FormatInt(size, 10)},
	})
	// The upload is over: this frees a writer that still waits.
	reader.CloseWithError(errors.New("the upload ended"))
	if cause := context.Cause(ctx); err != nil && cause != nil {
		err = cause
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
			return nil, err
		}
		for _, object := range page.Contents {
			id := copyIDOf(aws.ToString(object.Key), database)
			if at, ok := protocol.CopyTime(id); ok {
				copies = append(copies, protocol.Copy{ID: id, At: at, Size: aws.ToInt64(object.Size)})
			}
		}
	}
	slices.SortFunc(copies, func(x, y protocol.Copy) int { return y.At.Compare(x.At) })
	return copies, nil
}

// readCopy opens a copy in the bucket. The caller closes it.
func (b *backups) readCopy(ctx context.Context, app string, database protocol.Database, id string) (source, io.Closer, error) {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return source{}, nil, err
	}
	key := copyKey(folder, id, database)
	object, err := s3Client(bucket).GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket.Bucket), Key: key})
	if err != nil {
		return source{}, nil, fmt.Errorf("the copy %s of %s: %w", id, database.Path, err)
	}
	// No note means no number: only the spare room is checked.
	size, _ := strconv.ParseInt(object.Metadata[sizeNote], 10, 64)
	return source{copy: object.Body, size: size}, object.Body, nil
}

// deleteCopy deletes a copy in the bucket.
func (b *backups) deleteCopy(ctx context.Context, app string, database protocol.Database, id string) error {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return err
	}
	key := copyKey(folder, id, database)
	_, err = s3Client(bucket).DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: key})
	return err
}

// prune deletes the copies and undos older than the app keeps them,
// counted from the newest copy, never that one.
func (b *backups) prune(ctx context.Context, app string, database protocol.Database, copies []protocol.Copy) error {
	if len(copies) == 0 {
		return nil
	}
	_, keep := b.config().schedule(app)
	before := copies[0].At.Add(-keep)
	var err error
	for index, copy := range copies {
		if index > 0 && copy.At.Before(before) {
			err = errors.Join(err, b.deleteCopy(ctx, app, database, copy.ID))
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

// loop starts the work of the backups: the copies every minute, the daily
// checks, and every hour the search for new databases. Each runs on its
// own, so a copy that takes hours holds up nothing else.
func (b *backups) loop(ctx context.Context) {
	minute := time.NewTicker(time.Minute)
	defer minute.Stop()
	hourly := time.NewTimer(time.Minute)
	daily := time.NewTimer(checkWait(b.newestCheck()))
	for {
		select {
		case <-ctx.Done():
			return
		case <-minute.C:
			go b.tick(ctx)
		case <-hourly.C:
			go b.searchDatabases(ctx)
			hourly.Reset(time.Hour)
		case <-daily.C:
			go b.checkAll(ctx)
			daily.Reset(day)
		}
	}
}

// checkWait is how long a daemon that starts waits for its daily check:
// ten minutes when the last one is a day old or there was none, else
// until it is.
func checkWait(last time.Time) time.Duration {
	return min(max(time.Until(last.Add(day)), 10*time.Minute), day)
}

// newestCheck is when the last daily check of any database ran.
func (b *backups) newestCheck() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	var newest time.Time
	for _, s := range b.states {
		if s.CheckedAt.After(newest) {
			newest = s.CheckedAt
		}
	}
	return newest
}

// tick ends the restores a stopped command left, takes the copies that are
// due, deletes the old ones and writes down how it went.
func (b *backups) tick(ctx context.Context) {
	if !b.ticking.TryLock() {
		return
	}
	defer b.ticking.Unlock()
	b.recover(ctx)
	if !b.on() {
		return
	}
	for app, databases := range b.running() {
		for _, database := range databases {
			b.tickDatabase(ctx, app, database)
		}
	}
	b.saveStates()
}

// tickDatabase does what is due for one database and notes what failed.
func (b *backups) tickDatabase(ctx context.Context, app string, database protocol.Database) {
	b.mu.Lock()
	copied := b.state(app, database).LastCopy
	b.mu.Unlock()
	if b.busy(app) {
		return
	}
	var err error
	due, _ := b.config().schedule(app)
	// A copy from the future, after the clock jumped, is no reason to wait.
	if time.Since(copied) >= due || copied.After(time.Now()) {
		err = b.copyDue(ctx, app, database, due)
	}
	b.mu.Lock()
	s := b.state(app, database)
	switch {
	case err == nil:
		s.CopyError, s.FailingSince = "", time.Time{}
	default:
		slog.Error("backup: the copy failed", "app", app, "database", database.Path, "error", err)
		if s.CopyError == "" {
			s.FailingSince = time.Now().UTC()
		}
		s.CopyError = err.Error()
	}
	b.mu.Unlock()
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
		return err
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
		status.Error = s.CopyError
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
	for app, databases := range b.running() {
		for _, database := range databases {
			all[app] = append(all[app], b.status(ctx, app, database, false))
		}
		all[app] = append(all[app], b.cannotBackUp(app)...)
	}
	return all
}

// checkAll restores the newest copy of every database into a throwaway
// one and reads it.
func (b *backups) checkAll(ctx context.Context) {
	if !b.on() {
		return
	}
	for app, databases := range b.running() {
		for _, database := range databases {
			b.check(ctx, app, database)
		}
	}
	b.saveStates()
}

// check restores the newest copy of one database into a throwaway one,
// reads it and notes how it went. Without a copy there is nothing to
// check.
func (b *backups) check(ctx context.Context, app string, database protocol.Database) {
	copies, err := b.copies(ctx, app, database)
	if err != nil || len(copies) == 0 {
		return
	}
	from, body, err := b.readCopy(ctx, app, database, copies[0].ID)
	var gone *types.NoSuchKey
	if errors.As(err, &gone) {
		// A newer copy came and the old one went meanwhile.
		return
	}
	if err == nil {
		err = b.engine(app, database).Check(ctx, from)
		body.Close()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state(app, database)
	s.CheckedAt, s.CheckError = time.Now().UTC(), ""
	if err != nil {
		s.CheckError = "the copy " + copies[0].ID + " did not restore: " + err.Error()
		slog.Error("backup: the daily check failed", "app", app, "database", database.Path, "copy", copies[0].ID, "error", err)
		return
	}
	s.CheckedCopy = copies[0].ID
	slog.Info("backup: the daily check restored the newest copy", "app", app, "database", database.Path, "copy", copies[0].ID)
}

// copyBefore copies the app's databases, or the one named, just before a
// deploy or an edit, and returns when: the time in the copies' IDs; nil
// without backups or when none worked. logf says when one failed.
func (b *backups) copyBefore(ctx context.Context, app, only string, logf func(string, ...any)) *time.Time {
	if b == nil || !b.on() {
		return nil
	}
	// Two in one second would share an ID: the second waits for the next.
	at, took := time.Now().UTC().Truncate(time.Second), false
	b.mu.Lock()
	if !at.After(b.before[app]) {
		at = b.before[app].Add(time.Second)
	}
	b.before[app] = at
	b.mu.Unlock()
	time.Sleep(time.Until(at))
	for _, database := range b.databases(ctx, app) {
		if only == "" || database.Path == only {
			_, err := b.copy(ctx, app, database, protocol.BeforeCopyID(at))
			if err != nil {
				logf("the copy of %s before the change failed: %s", database.Path, err)
			}
			took = took || err == nil
		}
	}
	if !took {
		return nil
	}
	return &at
}

// editCopyLimit is the biggest database moin copies before an edit. A
// bigger one would keep the edit waiting; the newest scheduled copy is
// the way back.
const editCopyLimit = 1 << 30

// copyBeforeEdit copies a database just before an edit, unless it is
// too big for that. It returns when, nil when there is no such copy.
func (b *backups) copyBeforeEdit(ctx context.Context, app string, database protocol.Database, logf func(string, ...any)) *time.Time {
	if b == nil {
		return nil
	}
	size, err := b.engine(app, database).Size(ctx)
	if err != nil || size > editCopyLimit {
		return nil
	}
	return b.copyBefore(ctx, app, database.Path, logf)
}

// running are the databases of every app's live deploy.
func (b *backups) running() map[string][]protocol.Database {
	found := map[string][]protocol.Database{}
	entries, _ := os.ReadDir(b.apps.dir)
	for _, entry := range entries {
		databases := b.databases(context.Background(), entry.Name())
		if len(databases) > 0 || len(b.cannotBackUp(entry.Name())) > 0 {
			found[entry.Name()] = databases
		}
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

// restoreLock is the file a restore of app holds locked while it runs:
// the daemon copies nothing of the app meanwhile, and a restore whose
// command stopped is one that is written down as running without it.
func (b *backups) restoreLock(app string) string {
	return filepath.Join(filepath.Dir(b.apps.dir), "locks", app+".restore")
}

// restore puts a database back. It is written down before anything
// changes and ends with a result either way.
func (b *backups) restore(ctx context.Context, request protocol.BackupRestore) error {
	if !restoreID.MatchString(request.ID) {
		return errors.New("a restore needs an ID")
	}
	database, err := b.one(request.App, request.Database)
	if err != nil {
		return err
	}
	// Copies and edits wait from here on.
	unlockRestore, ok := tryFileLock(b.restoreLock(request.App))
	if !ok {
		return fmt.Errorf("another restore of %s runs: wait for it to end", request.App)
	}
	defer unlockRestore()
	mark := restoreMark{ID: request.ID, App: request.App, Database: database.Path, Undo: protocol.NewCopyID(time.Now()), From: request.Undo, State: protocol.RestoreRunning}
	if err := writeState(b.markFile(mark.ID), mark); err != nil {
		return fmt.Errorf("write the restore down: %w", err)
	}
	// A deploy that holds the lock ends first.
	unlock := b.apps.lock(request.App)
	from, body, err := b.restoreSource(ctx, request, database)
	if body != nil {
		defer body.Close()
	}
	if err == nil {
		err = b.engine(request.App, database).Restore(ctx, from, mark.Undo)
	}
	unlock()
	b.finish(mark, err)
	return err
}

// restoreSource is what a restore goes back to. Closing the second result,
// when there is one, ends reading the copy.
func (b *backups) restoreSource(ctx context.Context, request protocol.BackupRestore, database protocol.Database) (source, io.Closer, error) {
	if protocol.ValidCopyID(request.Copy) {
		return b.readCopy(ctx, request.App, database, request.Copy)
	}
	if protocol.ValidCopyID(request.Undo) {
		return source{undo: request.Undo}, nil, nil
	}
	return source{}, nil, errors.New("pick a copy or an undo to go back to")
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

func (b *backups) restoreStates(app string) []protocol.RestoreState {
	states := []protocol.RestoreState{}
	for _, mark := range b.marks() {
		if mark.App == app {
			states = append(states, protocol.RestoreState{ID: mark.ID, Database: mark.Database, State: mark.State, Error: mark.Error, FinishedAt: mark.FinishedAt})
		}
	}
	return states
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
		if unlock, ok := tryFileLock(b.restoreLock(mark.App)); ok {
			b.recoverRestore(ctx, mark)
			unlock()
		}
	}
}

// recoverRestore ends one restore a stopped daemon left running.
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
		slog.Error("backup: how a restore the daemon stopped in ended is not known yet", "app", mark.App, "database", mark.Database, "error", err)
		return
	}
	switch {
	case !switched:
		err = errors.New("the daemon stopped during the restore; the data is as it was before")
	case startErr != nil:
		err = fmt.Errorf("the restored data is in place, but start the app: %w", startErr)
	}
	b.finish(mark, err)
}

// sweepTemp removes what stopped copies and checks left.
func (b *backups) sweepTemp() {
	for _, databases := range b.running() {
		for _, database := range databases {
			if database.Postgres() {
				continue
			}
			if root, name, err := databaseFolder(database); err == nil {
				sweepTemp(root, name)
				root.Close()
			}
		}
	}
}

// busy says a restore works on an app: its data waits.
func (b *backups) busy(app string) bool {
	unlock, ok := tryFileLock(b.restoreLock(app))
	if ok {
		unlock()
	}
	return !ok
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
	if os.MkdirAll(filepath.Dir(name), 0o700) != nil {
		return nil, false
	}
	file, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
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
