package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/protocol"
)

// loaded is a database service that cannot switch to a database built
// beside the live one: MongoDB, ClickHouse and Redis. Its restore keeps
// the live database as a file on the server, the undo, and loads the
// copy over the live one. backup_mongo.go, backup_clickhouse.go and
// backup_redis.go are the three.
type loaded interface {
	// dump writes a copy of the live database to w.
	dump(ctx context.Context, w io.Writer) error
	// replace empties the live database and loads a copy into it. It
	// waits for a database that is still starting.
	replace(ctx context.Context, copy io.Reader) error
	// Size is how many bytes the live database takes.
	Size(ctx context.Context) (int64, error)
}

// overLive is the restore of a loaded database and its undo files. Each
// of the three engines has one, and gets its Restore, Recover, Undos and
// Drop from it. ending ends the name of an undo file, like ".rdb.gz".
type overLive struct {
	b        *backups
	app      string
	database protocol.Database
	db       loaded
	ending   string
}

// serviceContainer is the running container of an app's service and the
// exact image it runs. what names the database in an error, like "Redis".
func (a *apps) serviceContainer(ctx context.Context, app, service, what string) (container, image string, err error) {
	if !protocol.ValidAppName(app) || !validService(service) {
		return "", "", errors.New("invalid app or service name")
	}
	out, err := a.output(ctx, "docker", "ps", "-q", "--filter", "status=running",
		"--filter", "label=com.docker.compose.project="+a.projectOf(app),
		"--filter", "label=com.docker.compose.service="+service)
	if err != nil {
		return "", "", err
	}
	container, _, _ = strings.Cut(strings.TrimSpace(out), "\n")
	if container == "" {
		return "", "", fmt.Errorf("the %s service %s of %s is not running", what, service, app)
	}
	image, err = a.output(ctx, "docker", "inspect", "-f", "{{.Image}}", container)
	return container, strings.TrimSpace(image), err
}

// checkContainer names the throwaway container the daily check of an
// app's service runs in. remove takes it and its data away.
func (a *apps) checkContainer(ctx context.Context, app, service string) (name string, remove func()) {
	sum := sha256.Sum256([]byte(app + "/" + service))
	name = tempName("check-" + hex.EncodeToString(sum[:4]))
	return name, func() {
		_, _ = a.output(context.WithoutCancel(ctx), "docker", "rm", "-f", "-v", name)
	}
}

// roomForCheck says Docker's disk has room to load a copy of size.
func (a *apps) roomForCheck(ctx context.Context, size int64) error {
	folder, err := a.output(ctx, "docker", "info", "-f", "{{.DockerRootDir}}")
	if err != nil {
		return nil
	}
	return room(strings.TrimSpace(folder), size)
}

// waitFor asks answers every two seconds, up to three minutes.
func waitFor(ctx context.Context, answers func(context.Context) bool) error {
	for try := 0; try < 90; try++ {
		if answers(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return errors.New("it did not answer in three minutes")
}

// undoFile is where the state before the restore id is kept: a file in
// the app's folder on the server, which no app mounts.
func (o overLive) undoFile(id string) string {
	return filepath.Join(o.b.apps.appDir(o.app), "undos", o.database.Service, id+o.ending)
}

// Restore stops the app's other services, dumps the live database into
// the undo file and loads the copy, or an earlier undo, over the live
// database. A load that fails puts the undo back in, so the database is
// as it was. An undo it went back to is used up, like every engine's.
func (o overLive) Restore(ctx context.Context, from source, keepAs string) error {
	if from.undo != "" {
		file, err := os.Open(o.undoFile(from.undo))
		if err != nil {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
		defer file.Close()
		from.copy = file
	}
	folder := filepath.Dir(o.undoFile(keepAs))
	size, err := o.db.Size(ctx)
	if err == nil {
		err = os.MkdirAll(folder, 0o700)
	}
	if err == nil {
		err = room(folder, size)
	}
	if err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	}
	// An app that is only its database has no other service to stop.
	start := func() error { return nil }
	if others := o.b.otherServices(o.app, o.database.Service); len(others) > 0 {
		start, err = o.b.apps.stopServices(ctx, o.app, others...)
		if err != nil {
			return err
		}
	}
	ctx = context.WithoutCancel(ctx)
	if err := o.keep(ctx, keepAs); err != nil {
		return errors.Join(fmt.Errorf("keep the state before the restore: %w; nothing changed", err), start())
	}
	if err := o.db.replace(ctx, from.copy); err != nil {
		return errors.Join(o.back(ctx, keepAs, err), start())
	}
	if from.undo != "" {
		_ = os.Remove(o.undoFile(from.undo))
	}
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

// keep dumps the live database into the undo file of id. The file has
// its name only once it is whole and on the disk: an undo that is there
// says the load over the live database began.
func (o overLive) keep(ctx context.Context, id string) error {
	name := o.undoFile(id)
	file, err := os.OpenFile(name+".new", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	err = errors.Join(o.db.dump(ctx, file), file.Sync(), file.Close())
	if err == nil {
		err = os.Rename(name+".new", name)
	}
	if err == nil {
		err = syncFile(filepath.Dir(name))
	}
	if err != nil {
		_ = os.Remove(name + ".new")
	}
	return err
}

// back puts the undo of id into the live database again, after a load
// that failed with why, and removes the undo. When that fails too, the
// undo stays to go back to.
func (o overLive) back(ctx context.Context, id string, why error) error {
	file, err := os.Open(o.undoFile(id))
	if err == nil {
		defer file.Close()
		err = o.db.replace(ctx, file)
	}
	if err != nil {
		return fmt.Errorf("%w, and going back failed too (%w): the state before is the undo %s", why, err, id)
	}
	_ = os.Remove(o.undoFile(id))
	return fmt.Errorf("%w; the data is as it was before", why)
}

// Recover ends a restore that was cut off. With no undo of it, nothing
// had changed. With one, the load over the live database had begun and
// may have stopped halfway, so the undo goes back in: the restore never
// counts as switched.
func (o overLive) Recover(ctx context.Context, mark restoreMark) (bool, error) {
	_ = os.Remove(o.undoFile(mark.Undo) + ".new")
	if !exists(o.undoFile(mark.Undo)) {
		return false, nil
	}
	file, err := os.Open(o.undoFile(mark.Undo))
	if err != nil {
		return false, err
	}
	defer file.Close()
	if err := o.db.replace(ctx, file); err != nil {
		return false, err
	}
	return false, os.Remove(o.undoFile(mark.Undo))
}

// Undos are the undo files of the database, newest first.
func (o overLive) Undos(ctx context.Context) ([]protocol.Copy, error) {
	files, _ := filepath.Glob(o.undoFile("*"))
	undos := []protocol.Copy{}
	for _, file := range files {
		id := strings.TrimSuffix(filepath.Base(file), o.ending)
		if at, ok := protocol.CopyTime(id); ok {
			undos = append(undos, protocol.Copy{ID: id, At: at, Size: fileSize(file)})
		}
	}
	slices.SortFunc(undos, func(a, b protocol.Copy) int { return strings.Compare(b.ID, a.ID) })
	return undos, nil
}

func (o overLive) Drop(ctx context.Context, id string) error {
	err := os.Remove(o.undoFile(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
