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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/benbjohnson/litestream"

	"github.com/axadrn/moin/protocol"
)

// A backup is a way back for a database, in the user's own bucket; data
// goes from the server to the bucket, never through the hub. One engine
// per kind of database, with the same verbs:
//
//	SQLite    Litestream writes every change down (backup_sqlite.go)
//	Postgres  pg_dump copies it on the schedule (backup_postgres.go)
//
// Every day moin checks that the newest state restores.
type engine interface {
	// Restore builds the database beside the live one, stops the app,
	// switches in one atomic step, the live one kept as the undo id, and
	// starts the app. There is never a moment without a live database.
	Restore(ctx context.Context, from source, id string) error
	// Recover tidies after a restore the daemon stopped in and says
	// whether it had switched, from what is there.
	Recover(ctx context.Context, mark restoreMark) (switched bool, err error)
	// Check restores the newest state into a throwaway database and reads
	// it all.
	Check(ctx context.Context) error
	// Undos are the states before restores, newest first; Drop removes one.
	Undos(ctx context.Context) ([]protocol.Copy, error)
	Drop(ctx context.Context, id string) error
}

// source is what a restore goes back to: a copy from the bucket (Postgres,
// with size the room building it takes), a minute (SQLite) or the undo of
// an earlier restore.
type source struct {
	copy io.Reader
	at   time.Time
	undo string
	size int64
}

const (
	day = 24 * time.Hour
	// stallLimit stops an upload when no data has moved for this long. A
	// big database may take hours, as long as it keeps moving. An upload
	// that hangs would hold up every other one.
	stallLimit = 30 * time.Minute
	// sizeNote is the name of the note on each copy that says how many
	// bytes its database took. A restore needs that much room.
	sizeNote = "database-size"
)

// backups runs the schedule of every database on the server.
type backups struct {
	apps *apps
	send func(protocol.MessageType, string, any)

	mu        sync.Mutex
	bucket    *protocol.Bucket
	buckets   map[string]*protocol.Bucket
	schedules map[string]protocol.Schedule
	states    map[string]*state
	// restoring are the apps a restore works on: no copy, no edit;
	// paused the apps the hub cannot open the bucket of, with why.
	restoring map[string]int
	paused    map[string]string
	// before is the newest moment before a change of each app.
	before map[string]time.Time
	// stores and watched are Litestream's, see backup_sqlite.go.
	stores  map[int]*litestream.Store
	watched map[string]watched
	// ticking lets one round run at a time.
	ticking sync.Mutex
}

// state is what the daemon knows of one database: its newest copy
// (Postgres), why its backup fails, the newest daily check and what the
// hub heard last.
type state struct {
	lastCopy           time.Time
	copyError          string
	checkedAt          time.Time
	checkErr, reported string
}

func newBackups(a *apps, send func(protocol.MessageType, string, any)) *backups {
	return &backups{
		apps:      a,
		send:      send,
		states:    map[string]*state{},
		restoring: map[string]int{},
		paused:    map[string]string{},
		before:    map[string]time.Time{},
		stores:    map[int]*litestream.Store{},
		watched:   map[string]watched{},
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

// configure sets the buckets and schedules; a nil bucket stops backups.
func (b *backups) configure(config protocol.BackupConfig) {
	bucket := config.Bucket
	b.mu.Lock()
	b.bucket, b.buckets, b.schedules, b.paused = bucket, config.AppBuckets, config.Schedules, config.Paused
	b.mu.Unlock()
	if bucket == nil {
		b.forget(nil)
	}
}

func (b *backups) on() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bucket != nil
}

func (b *backups) schedule(app string) protocol.Schedule {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.schedules[app]; ok && s.Every > 0 && s.Keep > 0 {
		return s
	}
	return protocol.DefaultSchedule
}

// where is the bucket of a database and its folder in it:
// <path>/<workspace>/<app>/<app ID>/<database>.
func (b *backups) where(app string, database protocol.Database) (*protocol.Bucket, string, error) {
	b.mu.Lock()
	bucket, paused := b.buckets[app], b.paused[app]
	if bucket == nil {
		bucket = b.bucket
	}
	b.mu.Unlock()
	if paused != "" {
		return nil, "", errors.New(paused)
	}
	if bucket == nil {
		return nil, "", errors.New("backups are not set up: add a bucket under Data")
	}
	id := b.apps.appID(app)
	if id == "" {
		return nil, "", fmt.Errorf("deploy %s once more to start its backups", app)
	}
	return bucket, protocol.DatabaseFolder(*bucket, app, id, database.Path), nil
}

// copyKey is where a Postgres copy lives in its bucket.
func copyKey(folder, id string) *string {
	return aws.String(folder + "/" + protocol.CopyFile(id))
}

// copyIDOf is the ID of a Postgres copy, from its key in the bucket.
func copyIDOf(key string) string {
	return strings.TrimSuffix(path.Base(key), path.Base(protocol.CopyFile("")))
}

// copy streams a Postgres copy into the bucket as id.
func (b *backups) copy(ctx context.Context, app string, database protocol.Database, id string) (string, error) {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return "", err
	}
	at, _ := protocol.CopyTime(id)
	engine := &postgresEngine{b: b, app: app, database: database}
	size, err := engine.Size(ctx)
	if err != nil {
		return "", fmt.Errorf("copy %s: %w", database.Path, err)
	}
	note := map[string]string{sizeNote: strconv.FormatInt(size, 10)}
	if err := upload(ctx, bucket, *copyKey(folder, id), note, engine.Copy); err != nil {
		return "", fmt.Errorf("copy %s: %w", database.Path, err)
	}
	b.mu.Lock()
	if s := b.state(app, database); at.After(s.lastCopy) {
		s.lastCopy = at
	}
	b.mu.Unlock()
	return id, nil
}

// upload streams what write writes into the bucket at key, with notes on
// the object, and stops when no data moves.
func upload(ctx context.Context, bucket *protocol.Bucket, key string, notes map[string]string, write func(context.Context, io.Writer) error) error {
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	reader, writer := io.Pipe()
	go func() { writer.CloseWithError(write(ctx, writer)) }()
	body, stopWatching := stopWhenStuck(reader, stallLimit, stop)
	defer stopWatching()
	// Parts of 32 MB, two at a time: up to 320 GB in 64 MB of memory.
	uploader := manager.NewUploader(s3Client(bucket), func(u *manager.Uploader) { u.PartSize, u.Concurrency = 32<<20, 2 })
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key), Body: body, Metadata: notes})
	// The upload is over: this frees a writer that still waits.
	reader.CloseWithError(errors.New("the upload ended"))
	if cause := context.Cause(ctx); err != nil && cause != nil {
		err = cause
	}
	return err
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

// stopWhenStuck calls stop when no data has come from reader for limit.
// The second result ends the watching.
func stopWhenStuck(reader io.Reader, limit time.Duration, stop context.CancelCauseFunc) (io.Reader, func()) {
	timer := time.AfterFunc(limit, func() {
		stop(errors.New("no data moved for too long"))
	})
	return watchedReader{reader: reader, timer: timer, limit: limit}, func() { timer.Stop() }
}

// copies are the copies of a database in the bucket, newest first.
func (b *backups) copies(ctx context.Context, app string, database protocol.Database) ([]protocol.Copy, error) {
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
			id := copyIDOf(aws.ToString(object.Key))
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
	key := copyKey(folder, id)
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
	key := copyKey(folder, id)
	_, err = s3Client(bucket).DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: key})
	return err
}

// prune deletes the Postgres copies and undos older than the app keeps
// them, counted from the newest copy, never that one, and nothing of an
// app the hub sent no schedule for.
func (b *backups) prune(ctx context.Context, app string, database protocol.Database, copies []protocol.Copy) error {
	b.mu.Lock()
	_, scheduled := b.schedules[app]
	b.mu.Unlock()
	if !scheduled || len(copies) == 0 {
		return nil
	}
	before := copies[0].At.Add(-time.Duration(b.schedule(app).Keep) * day)
	var err error
	for index, copy := range copies {
		if index > 0 && copy.At.Before(before) {
			err = errors.Join(err, b.deleteCopy(ctx, app, database, copy.ID))
		}
	}
	return errors.Join(err, b.dropUndos(ctx, app, database, before))
}

// dropUndos deletes the undos older than before.
func (b *backups) dropUndos(ctx context.Context, app string, database protocol.Database, before time.Time) error {
	e := b.engine(app, database)
	undos, err := e.Undos(ctx)
	for _, undo := range undos {
		if undo.At.Before(before) {
			err = errors.Join(err, e.Drop(ctx, undo.ID))
		}
	}
	return err
}

// loop starts the work of the backups: the round every minute, the daily
// checks, and every hour the sweep of the bucket, the search for new
// databases and what SQLite no longer keeps. Each runs on its own, so a
// copy that takes hours holds up nothing else.
func (b *backups) loop(ctx context.Context) {
	minute := time.NewTicker(time.Minute)
	defer minute.Stop()
	hourly := time.NewTimer(time.Minute)
	daily := time.NewTimer(10 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-minute.C:
			go b.tick(ctx)
		case <-hourly.C:
			go b.sweepBucket(ctx)
			go b.searchDatabases(ctx)
			go b.pruneSQLite(ctx)
			hourly.Reset(time.Hour)
		case <-daily.C:
			go b.checkAll(ctx)
			daily.Reset(day)
		}
	}
}

// tick has Litestream follow the SQLite files, takes the Postgres copies
// that are due and deletes the old ones, and reports what changed.
func (b *backups) tick(ctx context.Context) {
	if !b.on() || !b.ticking.TryLock() {
		return
	}
	defer b.ticking.Unlock()
	running := b.running()
	for app, databases := range running {
		for _, database := range databases {
			b.tickDatabase(ctx, app, database)
		}
	}
	b.forget(running)
	b.report(ctx, running, false)
}

// tickDatabase does what is due for one database and notes what failed.
func (b *backups) tickDatabase(ctx context.Context, app string, database protocol.Database) {
	if b.busy(app) {
		return
	}
	var err error
	if database.Postgres() {
		err = b.copyIfDue(ctx, app, database)
	} else {
		err = b.follow(ctx, app, database)
	}
	failed := ""
	if err != nil {
		failed = err.Error()
	}
	b.mu.Lock()
	b.state(app, database).copyError = failed
	b.mu.Unlock()
}

// copyIfDue takes a Postgres copy when its time came.
func (b *backups) copyIfDue(ctx context.Context, app string, database protocol.Database) error {
	b.mu.Lock()
	copied := b.state(app, database).lastCopy
	b.mu.Unlock()
	due := time.Duration(b.schedule(app).Every) * time.Minute
	// A copy from the future, after the clock jumped, is no reason to wait.
	if time.Since(copied) >= due || copied.After(time.Now()) {
		return b.copyDue(ctx, app, database, due)
	}
	return nil
}

// searchDatabases looks for new databases in every app and tells the hub.
func (b *backups) searchDatabases(ctx context.Context) {
	entries, _ := os.ReadDir(b.apps.dir)
	for _, entry := range entries {
		app := entry.Name()
		detected, changed := b.apps.findNewDatabases(ctx, app)
		if changed && b.send != nil {
			b.send(protocol.TypeBackupStatus, "", protocol.Backups{App: app, Databases: []protocol.DatabaseBackup{}, Restores: b.restoreStates(app), Detected: &detected})
		}
	}
	if b.on() {
		b.report(ctx, b.running(), true)
	}
}

// copyDue takes a Postgres copy when the newest in the bucket is older than due,
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
		b.state(app, database).lastCopy = copies[0].At
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

// report sends how the databases of each app stand, when that changed or
// always.
func (b *backups) report(ctx context.Context, running map[string][]protocol.Database, always bool) {
	for app, databases := range running {
		changed := always
		statuses := []protocol.DatabaseBackup{}
		for _, database := range databases {
			status := b.status(ctx, app, database, false)
			b.mu.Lock()
			s := b.state(app, database)
			changed = changed || s.reported != status.Error
			s.reported = status.Error
			b.mu.Unlock()
			statuses = append(statuses, status)
		}
		statuses = append(statuses, b.cannotBackUp(app)...)
		if changed && b.send != nil {
			b.send(protocol.TypeBackupStatus, "", protocol.Backups{App: app, Databases: statuses, Restores: b.restoreStates(app)})
		}
	}
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

// status is how one database is backed up; full also reads the undos and
// the copies (Postgres), or how far Every change reaches (SQLite).
func (b *backups) status(ctx context.Context, app string, database protocol.Database, full bool) protocol.DatabaseBackup {
	status := protocol.DatabaseBackup{Database: database.Path, Kind: database.Kind}
	b.mu.Lock()
	s := *b.state(app, database)
	b.mu.Unlock()
	if !s.lastCopy.IsZero() {
		status.CopiedAt = &s.lastCopy
	}
	if !s.checkedAt.IsZero() {
		status.CheckedAt, status.CheckError = &s.checkedAt, s.checkErr
	}
	status.Error = s.copyError
	// Copies that stopped coming are a problem too, with an alert.
	if status.Error == "" && b.late(app, s.lastCopy) {
		status.Error = "the copies are late: the newest is from " + s.lastCopy.UTC().Format("Jan 2 15:04 UTC")
	}
	if !full {
		return status
	}
	if database.Postgres() {
		copies, err := b.copies(ctx, app, database)
		if err != nil && status.Error == "" {
			status.Error = err.Error()
		}
		if status.Copies = copies; len(copies) > 0 {
			status.CopiedAt = &copies[0].At
		}
	} else {
		status.Since, status.Until = b.reach(ctx, app, database)
	}
	status.Undos, _ = b.engine(app, database).Undos(ctx)
	return status
}

// late says the newest copy is older than twice how often the app is
// copied, and than one more copy may take.
func (b *backups) late(app string, copied time.Time) bool {
	if copied.IsZero() {
		return false
	}
	every := time.Duration(b.schedule(app).Every) * time.Minute
	return time.Since(copied) > max(2*every, every+time.Hour)
}

// errNoState says a database has nothing to check yet.
var errNoState = errors.New("nothing to check yet")

// checkAll restores the newest state of every database into a throwaway
// one and reads it.
func (b *backups) checkAll(ctx context.Context) {
	running := b.running()
	for app, databases := range running {
		for _, database := range databases {
			if _, _, err := b.where(app, database); err != nil {
				continue
			}
			err := b.engine(app, database).Check(ctx)
			if errors.Is(err, errNoState) {
				continue
			}
			b.mu.Lock()
			s := b.state(app, database)
			s.checkedAt, s.checkErr = time.Now().UTC(), ""
			if err != nil {
				s.checkErr = "the daily check failed: " + err.Error()
				slog.Error("backup: the daily check failed", "app", app, "database", database.Path, "error", err)
			}
			b.mu.Unlock()
		}
	}
	b.report(ctx, running, true)
}

// copyBefore remembers the moment just before a deploy or an edit of the
// app's databases, or of the one named: SQLite can go back to that moment
// as it is, Postgres gets a copy. It returns when, the time in the copies'
// IDs; nil without backups or when no moment could be kept. logf says when
// a copy failed.
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
		if only != "" && database.Path != only {
			continue
		}
		if !database.Postgres() {
			took = true
			continue
		}
		_, err := b.copy(ctx, app, database, protocol.BeforeCopyID(at))
		if err != nil {
			logf("the copy of %s before the change failed: %s", database.Path, err)
		}
		took = took || err == nil
	}
	if !took {
		return nil
	}
	return &at
}

// editCopyLimit is the biggest Postgres database moin copies before an
// edit. A bigger one would keep the edit waiting; the newest scheduled
// copy is the way back.
const editCopyLimit = 1 << 30

// copyBeforeEdit remembers the moment just before an edit. A Postgres
// database too big to copy has none; it returns nil then.
func (b *backups) copyBeforeEdit(ctx context.Context, app string, database protocol.Database, logf func(string, ...any)) *time.Time {
	if b == nil {
		return nil
	}
	if database.Postgres() {
		size, err := (&postgresEngine{b: b, app: app, database: database}).Size(ctx)
		if err != nil || size > editCopyLimit {
			return nil
		}
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

// list answers backup.list.
func (b *backups) list(ctx context.Context, request protocol.BackupRequest) protocol.Backups {
	result := protocol.Backups{App: request.App, Databases: []protocol.DatabaseBackup{}, Restores: b.restoreStates(request.App)}
	for _, database := range b.databases(ctx, request.App) {
		if request.Database == "" || request.Database == database.Path {
			result.Databases = append(result.Databases, b.status(ctx, request.App, database, true))
		}
	}
	for _, status := range b.cannotBackUp(request.App) {
		if request.Database == "" || request.Database == status.Database {
			result.Databases = append(result.Databases, status)
		}
	}
	return result
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

// restore puts a database back. It is written down before anything
// changes and ends with a result either way.
func (b *backups) restore(ctx context.Context, request protocol.BackupRestore) error {
	if !restoreID.MatchString(request.ID) {
		return errors.New("a restore needs an ID: update the hub")
	}
	database, err := b.one(request.App, request.Database)
	if err != nil {
		return err
	}
	mark := restoreMark{ID: request.ID, App: request.App, Database: database.Path, Undo: protocol.NewCopyID(time.Now()), From: request.Undo, State: protocol.RestoreRunning}
	if err := writeState(b.markFile(mark.ID), mark); err != nil {
		return fmt.Errorf("write the restore down: %w", err)
	}
	// Edits are refused from here on, and those that hold the lock end
	// first.
	b.mu.Lock()
	b.restoring[request.App]++
	b.mu.Unlock()
	unlock := b.apps.lock(request.App)
	from, body, err := b.restoreSource(ctx, request, database)
	if body != nil {
		defer body.Close()
	}
	if err == nil {
		err = b.engine(request.App, database).Restore(ctx, from, mark.Undo)
	}
	// In this order: an edit that waited for the lock still sees the
	// restore and is refused.
	unlock()
	b.mu.Lock()
	b.restoring[request.App]--
	b.mu.Unlock()
	b.finish(mark, err)
	go b.report(context.WithoutCancel(ctx), map[string][]protocol.Database{request.App: {database}}, true)
	return err
}

// restoreSource is what a restore goes back to. Closing the second result,
// when there is one, ends reading the copy.
func (b *backups) restoreSource(ctx context.Context, request protocol.BackupRestore, database protocol.Database) (source, io.Closer, error) {
	if protocol.ValidCopyID(request.Copy) && database.Postgres() {
		return b.readCopy(ctx, request.App, database, request.Copy)
	}
	if protocol.ValidCopyID(request.Undo) {
		return source{undo: request.Undo}, nil, nil
	}
	if request.At != nil && !database.Postgres() {
		return source{at: request.At.UTC()}, nil, nil
	}
	return source{}, nil, errors.New("pick a copy, a minute or an undo to go back to")
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

// recover ends the restores a stopped daemon left running: the database
// is whole either way, the app starts again, and the result says which
// database it runs on. When it cannot tell, the app starts all the same,
// and the mark stays for the next start to tell. Then it sweeps what
// stopped checks and downloads left. The daemon runs it when it starts.
func (b *backups) recover(ctx context.Context) {
	for _, mark := range b.marks() {
		if mark.State == protocol.RestoreRunning {
			b.recoverRestore(ctx, mark)
		}
	}
	b.sweepTemp()
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
	startErr := b.apps.compose(context.WithoutCancel(ctx), mark.App, b.apps.composeFile(mark.App), func(string, ...any) {}, "start")
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

// sweepTemp removes what stopped checks and downloads left.
func (b *backups) sweepTemp() {
	for _, databases := range b.running() {
		for _, database := range databases {
			if !database.Postgres() {
				sweepTemp(database.HostPath)
			}
		}
	}
}

// busy says a restore works on an app: its data waits.
func (b *backups) busy(app string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.restoring[app] > 0
}

// writeState writes a file whole: a temp file, synced, renamed over the
// old one, the folder synced. A crash leaves the old file or the new one.
func writeState(name string, value any) error {
	body, _ := json.Marshal(value)
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
