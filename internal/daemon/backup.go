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
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/benbjohnson/litestream"

	"github.com/axadrn/moin/protocol"
)

// A backup is a copy of a database in the user's own bucket, taken here
// on the server on the app's schedule and kept as long as the app says;
// every day moin checks that the newest one restores. Data goes from the
// server to the bucket, never through the hub. One engine per
// kind of database, with the same verbs:
//
//	database ── Copy ───▶ bucket <database>/copies/<id>
//	copy ────── Check ──▶ a throwaway database, read through
//	copy ────── Restore ▶ built beside the live one, one switch,
//	                      the live one kept as the undo
//
// backup_sqlite.go copies with VACUUM INTO and records every change with
// Litestream, backup_postgres.go copies with pg_dump.
type engine interface {
	// Copy writes a full copy of the live database to w.
	Copy(ctx context.Context, w io.Writer) error
	// Restore builds the database beside the live one, stops the app,
	// renames the live one aside as the undo id and the built one in,
	// and starts the app. It calls step with "switching" before the
	// first rename, "aside" after it and "switched" after the second, so
	// the mark says how far it came.
	Restore(ctx context.Context, from source, id string, step func(string) error) error
	// Recover is the crash rule of the restore a mark names, decided by
	// the step it reached (see recoverFrom). switched says the restore
	// had switched.
	Recover(ctx context.Context, mark restoreMark) (switched bool, err error)
	// Check restores a copy into a throwaway database and reads it all.
	Check(ctx context.Context, copy io.Reader) error
	// Undos are the states before restores, newest first; Drop removes one.
	Undos(ctx context.Context) ([]protocol.Copy, error)
	Drop(ctx context.Context, id string) error
}

// source is what a restore goes back to: a copy from the bucket, a
// minute (SQLite with Every change) or the undo of an earlier restore.
type source struct {
	copy io.Reader
	at   time.Time
	undo string
}

const (
	day = 24 * time.Hour
	// copyTimeout ends a copy that hangs: it holds up every other copy,
	// and the deploys and edits that wait for theirs.
	copyTimeout = time.Hour
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
	// restoring are the apps a restore works on: no copy, no edit.
	restoring map[string]bool
	// stores and watched are Litestream's, see backup_sqlite.go.
	stores  map[int]*litestream.Store
	watched map[string]watched
	// copying takes one copy at a time, ticking one round.
	copying, ticking sync.Mutex
	// crash ends a restore at a step as a killed daemon would, in tests.
	crash func(step string) bool
}

// state is what the daemon knows of one database: its newest copy, why
// the last one failed, the newest daily check and what the hub heard
// last.
type state struct {
	copied             time.Time
	failed             string
	checkedAt          time.Time
	checkErr, reported string
}

var errCrash = errors.New("the daemon stopped here")

func newBackups(a *apps, send func(protocol.MessageType, string, any)) *backups {
	return &backups{apps: a, send: send, states: map[string]*state{}, restoring: map[string]bool{}, stores: map[int]*litestream.Store{}, watched: map[string]watched{}}
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

func newCopyID() string { return time.Now().UTC().Format(protocol.CopyIDLayout) }

// configure sets the buckets and schedules; a nil bucket stops backups.
func (b *backups) configure(bucket *protocol.Bucket, buckets map[string]*protocol.Bucket, schedules map[string]protocol.Schedule) {
	b.mu.Lock()
	b.bucket, b.buckets, b.schedules = bucket, buckets, schedules
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
	bucket := b.buckets[app]
	if bucket == nil {
		bucket = b.bucket
	}
	b.mu.Unlock()
	if bucket == nil {
		return nil, "", errors.New("backups are not set up: add a bucket under Data")
	}
	id := b.apps.appID(app)
	if id == "" {
		return nil, "", fmt.Errorf("deploy %s once more to start its backups", app)
	}
	return bucket, protocol.DatabaseFolder(*bucket, app, id, database.Path), nil
}

// copy streams a copy of a database into the bucket as id.
func (b *backups) copy(ctx context.Context, app string, database protocol.Database, id string) error {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return err
	}
	b.copying.Lock()
	defer b.copying.Unlock()
	ctx, cancel := context.WithTimeout(ctx, copyTimeout)
	defer cancel()
	reader, writer := io.Pipe()
	go func() { writer.CloseWithError(b.engine(app, database).Copy(ctx, writer)) }()
	// Parts of 32 MB, two at a time: up to 320 GB in 64 MB of memory.
	uploader := manager.NewUploader(s3Client(bucket), func(u *manager.Uploader) { u.PartSize, u.Concurrency = 32<<20, 2 })
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(folder + "/" + protocol.CopyFile(id, database.Postgres())), Body: reader})
	reader.CloseWithError(errors.New("the upload ended"))
	if err != nil {
		return fmt.Errorf("copy %s: %w", database.Path, err)
	}
	at, _ := time.Parse(protocol.CopyIDLayout, id)
	b.mu.Lock()
	if s := b.state(app, database); at.After(s.copied) {
		s.copied = at
	}
	b.mu.Unlock()
	return nil
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
			id := strings.TrimSuffix(path.Base(aws.ToString(object.Key)), path.Base(protocol.CopyFile("", database.Postgres())))
			if at, err := time.Parse(protocol.CopyIDLayout, id); err == nil {
				copies = append(copies, protocol.Copy{ID: id, At: at, Size: aws.ToInt64(object.Size)})
			}
		}
	}
	slices.SortFunc(copies, func(x, y protocol.Copy) int { return y.At.Compare(x.At) })
	return copies, nil
}

// object reads or deletes the copy id of a database in the bucket.
func (b *backups) object(ctx context.Context, app string, database protocol.Database, id string, remove bool) (io.ReadCloser, error) {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return nil, err
	}
	name := aws.String(folder + "/" + protocol.CopyFile(id, database.Postgres()))
	if remove {
		_, err := s3Client(bucket).DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: name})
		return nil, err
	}
	object, err := s3Client(bucket).GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket.Bucket), Key: name})
	if err != nil {
		return nil, fmt.Errorf("the copy %s of %s: %w", id, database.Path, err)
	}
	return object.Body, nil
}

// prune deletes the copies and undos older than the app keeps them, but
// never the newest copy.
func (b *backups) prune(ctx context.Context, app string, database protocol.Database, copies []protocol.Copy) error {
	before := time.Now().Add(-time.Duration(b.schedule(app).Keep) * day)
	var err error
	for index, copy := range copies {
		if index > 0 && copy.At.Before(before) {
			_, deleteErr := b.object(ctx, app, database, copy.ID, true)
			err = errors.Join(err, deleteErr)
		}
	}
	if !database.Postgres() {
		err = errors.Join(err, b.pruneGenerations(ctx, app, database, before))
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

// loop runs tick every minute, the daily checks, and the sweep of the
// uploads every hour.
func (b *backups) loop(ctx context.Context) {
	ticker, checks := time.NewTicker(time.Minute), time.NewTimer(10*time.Minute)
	defer ticker.Stop()
	for round := 0; ; round++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.tick(ctx)
			if round%60 == 0 {
				b.sweepUploads(ctx)
			}
		case <-checks.C:
			b.checkAll(ctx)
			checks.Reset(day)
		}
	}
}

// tick takes the copies that are due and deletes the old ones, has
// Litestream follow SQLite files with Every change, and reports what
// changed.
func (b *backups) tick(ctx context.Context) {
	if !b.on() || !b.ticking.TryLock() {
		return
	}
	defer b.ticking.Unlock()
	running := b.running()
	for app, databases := range running {
		schedule := b.schedule(app)
		for _, database := range databases {
			b.mu.Lock()
			s, busy := *b.state(app, database), b.restoring[app]
			b.mu.Unlock()
			if busy {
				continue
			}
			var err error
			if !database.Postgres() {
				err = b.record(ctx, app, database, schedule)
			}
			// A copy from the future, after the clock jumped, is no reason
			// to wait.
			if due := time.Duration(schedule.Every) * time.Minute; time.Since(s.copied) >= due || s.copied.After(time.Now()) {
				err = errors.Join(err, b.copyDue(ctx, app, database, due))
			}
			b.mu.Lock()
			b.state(app, database).failed = ""
			if err != nil {
				b.state(app, database).failed = err.Error()
			}
			b.mu.Unlock()
		}
	}
	b.forget(running)
	b.report(ctx, running, false)
}

// copyDue takes a copy when the newest in the bucket is older than due,
// and deletes the old ones.
func (b *backups) copyDue(ctx context.Context, app string, database protocol.Database, due time.Duration) error {
	copies, err := b.copies(ctx, app, database)
	if err != nil {
		return err
	}
	if len(copies) > 0 && time.Since(copies[0].At) < due && !copies[0].At.After(time.Now()) {
		b.mu.Lock()
		b.state(app, database).copied = copies[0].At
		b.mu.Unlock()
		return nil
	}
	id := newCopyID()
	if err := b.copy(ctx, app, database, id); err != nil {
		return err
	}
	at, _ := time.Parse(protocol.CopyIDLayout, id)
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
		if changed && b.send != nil {
			b.send(protocol.TypeBackupStatus, "", protocol.Backups{App: app, Databases: statuses, Restores: b.restoreStates(app)})
		}
	}
}

// status is how one database is backed up; full also reads the copies,
// the undos and how far Every change reaches.
func (b *backups) status(ctx context.Context, app string, database protocol.Database, full bool) protocol.DatabaseBackup {
	status := protocol.DatabaseBackup{Database: database.Path, Kind: database.Kind}
	b.mu.Lock()
	s := *b.state(app, database)
	b.mu.Unlock()
	if !s.copied.IsZero() {
		status.CopiedAt = &s.copied
	}
	if !s.checkedAt.IsZero() {
		status.CheckedAt, status.CheckError = &s.checkedAt, s.checkErr
	}
	status.Error = s.failed
	// Copies that stopped coming are a problem too, with an alert.
	if every := time.Duration(b.schedule(app).Every) * time.Minute; status.Error == "" && !s.copied.IsZero() && time.Since(s.copied) > 2*every {
		status.Error = "the copies are late: the newest is from " + s.copied.UTC().Format("Jan 2 15:04 UTC")
	}
	if full {
		copies, err := b.copies(ctx, app, database)
		if err != nil && status.Error == "" {
			status.Error = err.Error()
		}
		if status.Copies = copies; len(copies) > 0 {
			status.CopiedAt = &copies[0].At
		}
		status.Undos, _ = b.engine(app, database).Undos(ctx)
		if !database.Postgres() && b.schedule(app).EveryChange {
			status.Since, status.Until = b.reach(ctx, app, database)
		}
	}
	return status
}

// checkAll restores the newest copy of every database into a throwaway
// one and reads it.
func (b *backups) checkAll(ctx context.Context) {
	running := b.running()
	for app, databases := range running {
		for _, database := range databases {
			copies, err := b.copies(ctx, app, database)
			if err != nil || len(copies) == 0 {
				continue
			}
			body, err := b.object(ctx, app, database, copies[0].ID, false)
			var gone *types.NoSuchKey
			if errors.As(err, &gone) {
				// A newer copy came and the old one went meanwhile.
				continue
			}
			if err == nil {
				err = b.engine(app, database).Check(ctx, body)
				body.Close()
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

// copyBefore copies the app's databases, or the one named, just before a
// deploy or an edit, and returns the ID of the copies; nil without
// backups or when none worked. logf says when one failed.
func (b *backups) copyBefore(ctx context.Context, app, only string, logf func(string, ...any)) *time.Time {
	if b == nil || !b.on() {
		return nil
	}
	id, took := newCopyID(), false
	for _, database := range b.databases(ctx, app) {
		if only == "" || database.Path == only {
			err := b.copy(ctx, app, database, id)
			if err != nil {
				logf("the copy of %s before the change failed: %s", database.Path, err)
			}
			took = took || err == nil
		}
	}
	at, _ := time.Parse(protocol.CopyIDLayout, id)
	if !took {
		return nil
	}
	return &at
}

// running are the databases of every app's live deploy.
func (b *backups) running() map[string][]protocol.Database {
	found := map[string][]protocol.Database{}
	entries, _ := os.ReadDir(b.apps.dir)
	for _, entry := range entries {
		if databases := b.databases(context.Background(), entry.Name()); len(databases) > 0 {
			found[entry.Name()] = databases
		}
	}
	return found
}

// one is the database a request names, or the app's only one.
func (b *backups) one(app, name string) (protocol.Database, error) {
	all := b.apps.runningDatabases(app)
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
	return result
}

// restoreMark is a restore in restores/<id>.json in the daemon's folder,
// written whole before anything changes, so a start after a crash knows
// it ran. Undo names the state before, which it leaves; From the undo it
// goes back to, if it does; Step how far it came.
type restoreMark struct {
	ID         string     `json:"id"`
	App        string     `json:"app"`
	Database   string     `json:"database"`
	Undo       string     `json:"undo"`
	From       string     `json:"from,omitempty"`
	Step       string     `json:"step,omitempty"`
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
	mark := restoreMark{ID: request.ID, App: request.App, Database: database.Path, Undo: newCopyID(), From: request.Undo, State: protocol.RestoreRunning}
	if err := writeState(b.markFile(mark.ID), mark); err != nil {
		return fmt.Errorf("write the restore down: %w", err)
	}
	defer b.apps.lock(request.App)()
	b.mu.Lock()
	b.restoring[request.App] = true
	b.mu.Unlock()
	var from source
	switch {
	case protocol.ValidCopyID(request.Copy):
		var body io.ReadCloser
		if body, err = b.object(ctx, request.App, database, request.Copy, false); err == nil {
			defer body.Close()
			from.copy = body
		}
	case protocol.ValidCopyID(request.Undo):
		from.undo = request.Undo
	case request.At != nil && !database.Postgres():
		from.at = request.At.UTC()
	default:
		err = errors.New("pick a copy, a minute or an undo to go back to")
	}
	// Each step is written down before the next: the crash rule reads it.
	step := func(reached string) error {
		mark.Step = reached
		if err := writeState(b.markFile(mark.ID), mark); err != nil {
			return err
		}
		if b.crash != nil && b.crash(reached) {
			return errCrash
		}
		return nil
	}
	if err == nil {
		err = b.engine(request.App, database).Restore(ctx, from, mark.Undo, step)
	}
	b.mu.Lock()
	delete(b.restoring, request.App)
	b.mu.Unlock()
	if errors.Is(err, errCrash) {
		return err
	}
	b.finish(mark, err)
	go b.report(context.WithoutCancel(ctx), map[string][]protocol.Database{request.App: {database}}, true)
	return err
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

// recover ends the restores a stopped daemon left running: the crash rule
// puts a whole database in place, the app starts, the result is written
// down. When the rule cannot act, the mark stays and the next start
// tries again. Then it sweeps what stopped copies and checks left. The
// daemon runs it when it starts.
func (b *backups) recover(ctx context.Context) {
	for _, mark := range b.marks() {
		if mark.State != protocol.RestoreRunning {
			continue
		}
		database, err := b.one(mark.App, mark.Database)
		switched := false
		if err == nil {
			switched, err = b.engine(mark.App, database).Recover(ctx, mark)
		}
		if err != nil {
			slog.Error("backup: a restore the daemon stopped in is not ended", "app", mark.App, "database", mark.Database, "error", err)
			continue
		}
		start := b.apps.compose(context.WithoutCancel(ctx), mark.App, b.apps.composeFile(mark.App), func(string, ...any) {}, "start")
		switch {
		case !switched:
			err = errors.New("the daemon stopped during the restore; the data is as it was before")
		case start != nil:
			err = fmt.Errorf("the restored data is in place, but start the app: %w", start)
		}
		b.finish(mark, err)
	}
	for _, databases := range b.running() {
		for _, database := range databases {
			if !database.Postgres() {
				sweepTemp(database.HostPath)
			}
		}
	}
}

// recoverFrom is the crash rule, the same for every engine: before
// "switching" nothing moved; at "switched" the restore is done. Between,
// the aside missing means nothing moved, the built one still there means
// the second rename never happened, so the aside goes back, and what
// stands in the live place meanwhile (an app that made an empty database
// on its start) goes aside as an undo of its own. Else it switched.
func recoverFrom(step string, aside, built bool, putBack func() error) (bool, error) {
	switch {
	case step == "", step != "switched" && !aside:
		return false, nil
	case step != "switched" && built:
		return false, putBack()
	}
	return true, nil
}

// strayID names the undo of what stood in the live place during a
// restore: a second after the restore's own undo, never the same.
func strayID(undo string) string {
	at, _ := time.Parse(protocol.CopyIDLayout, undo)
	return at.Add(time.Second).Format(protocol.CopyIDLayout)
}

// busy says a restore works on an app: its data waits.
func (b *backups) busy(app string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.restoring[app]
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
