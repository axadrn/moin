package daemon

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// What the CLI asks about backups, and the parts of the config it changes:
// the bucket, the alerts and the watchers. Each runs on the server as
// `moin here …`, as root. The daemon reads the config again every minute,
// so nothing here has to tell it.

// undoPrefix is what the CLI writes before the ID of an undo: restore
// --to undo-20261127T140000Z-482913.
const undoPrefix = "undo-"

// BackupsHere says how the databases of app are backed up: the copies in
// the bucket, the undos of earlier restores, and the last daily check with
// the copy it proved.
func BackupsHere(ctx context.Context, dir, app string) ([]protocol.DatabaseBackup, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return nil, err
	}
	b := a.backups
	statuses := []protocol.DatabaseBackup{}
	for _, database := range b.databases(ctx, app) {
		statuses = append(statuses, b.status(ctx, app, database, true))
	}
	return append(statuses, b.cannotBackUp(app)...), nil
}

// Copied is one copy BackupHere took.
type Copied struct {
	Database string `json:"database"`
	Copy     string `json:"copy"`
}

// BackupHere copies every database of app into the bucket now.
func BackupHere(ctx context.Context, dir, app string, out io.Writer) ([]Copied, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return nil, err
	}
	b := a.backups
	if !b.on() {
		return nil, errNoBucket
	}
	databases := b.databases(ctx, app)
	if len(databases) == 0 {
		return nil, fmt.Errorf("%s has no database moin can copy", app)
	}
	if b.busy(app) {
		return nil, fmt.Errorf("a restore of %s runs: copy it when it is done", app)
	}
	copied := []Copied{}
	for _, database := range databases {
		fmt.Fprintf(out, "copying %s\n", database.Path)
		id, err := b.copy(ctx, app, database, protocol.NewCopyID(time.Now()))
		if err != nil {
			return copied, err
		}
		copied = append(copied, Copied{Database: database.Path, Copy: id})
	}
	return copied, nil
}

// Restored is how a restore ended: what the database went back to, and the
// undo that brings back what it was before.
type Restored struct {
	Database string `json:"database"`
	To       string `json:"to"`
	Undo     string `json:"undo"`
}

// RestoreHere puts a database of app back: to a copy by its ID, to the
// newest copy at or before a time like 2026-11-27T14:07 (UTC), or to
// undo-<id>, the state before an earlier restore. name is the database,
// "" for an app that has one.
func RestoreHere(ctx context.Context, dir, app, name, to string, out io.Writer) (Restored, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Restored{}, err
	}
	b := a.backups
	database, err := b.one(app, name)
	if err != nil {
		return Restored{}, err
	}
	request := protocol.BackupRestore{ID: newRestoreID(), App: app, Database: database.Path}
	if undo, ok := strings.CutPrefix(to, undoPrefix); ok {
		request.Undo = undo
	} else {
		request.Copy, err = b.copyFor(ctx, app, database, to)
	}
	if err != nil {
		return Restored{}, err
	}
	fmt.Fprintf(out, "restoring %s to %s\n", database.Path, cmp.Or(request.Copy, to))
	if err := b.restore(ctx, request); err != nil {
		return Restored{}, err
	}
	restored := Restored{Database: database.Path, To: cmp.Or(request.Copy, to)}
	for _, mark := range b.marks() {
		if mark.ID == request.ID {
			restored.Undo = undoPrefix + mark.Undo
		}
	}
	return restored, nil
}

// newRestoreID names one restore in the daemon's notes.
func newRestoreID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// timeLayouts are how a restore names a moment, in UTC.
var timeLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"}

// copyFor is the copy a restore names: its ID, or the newest copy at or
// before a time.
func (b *backups) copyFor(ctx context.Context, app string, database protocol.Database, to string) (string, error) {
	if protocol.ValidCopyID(to) {
		return to, nil
	}
	var at time.Time
	var err error
	for _, layout := range timeLayouts {
		if at, err = time.Parse(layout, to); err == nil {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("%q is not a copy, a time like 2026-11-27T14:07 (UTC) or an undo. See them with `%s backups <server>`", to, build.Name)
	}
	copies, err := b.copies(ctx, app, database)
	if err != nil {
		return "", err
	}
	for _, copy := range copies {
		if !copy.At.After(at) {
			return copy.ID, nil
		}
	}
	return "", fmt.Errorf("%s has no copy from %s or before", database.Path, at.UTC().Format("Jan 2 15:04 UTC"))
}

// BucketHere is the server's bucket without its secret key, nil for none.
func BucketHere() (*Bucket, error) {
	config, err := LoadConfig()
	if err != nil || config.Bucket == nil {
		return nil, err
	}
	bucket := *config.Bucket
	bucket.SecretKey = ""
	return &bucket, nil
}

// SetBucketHere makes bucket the server's bucket, once a test file went in
// and out with its keys.
func SetBucketHere(ctx context.Context, bucket Bucket) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	config.Bucket = &bucket
	if err := config.check(); err != nil {
		return err
	}
	if err := testBucket(ctx, config); err != nil {
		return fmt.Errorf("moin could not write to the bucket with these keys: %s", lastLine(err.Error()))
	}
	return SaveConfig(config)
}

// AlertsHere names where the server's alerts go, without a token or the
// secret part of a URL.
func AlertsHere() ([]string, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, destination := range config.Alerts {
		names = append(names, alertName(destination))
	}
	return names, nil
}

// alertName names a destination for people: its kind and where it goes.
func alertName(d alert.Destination) string {
	if d.Kind == "telegram" {
		return "telegram chat " + d.ChatID
	}
	if link, err := url.Parse(d.URL); err == nil && link.Host != "" {
		return d.Kind + " " + link.Host
	}
	return d.Kind
}

// AddAlertHere adds a destination for alerts, once a test alert reached it.
func AddAlertHere(ctx context.Context, destination alert.Destination) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	if err := alert.Send(ctx, destination, testAlert(config)); err != nil {
		return fmt.Errorf("the test alert did not go out, so nothing changed: %w", err)
	}
	config.Alerts = append(config.Alerts, destination)
	return SaveConfig(config)
}

// RemoveAlertHere removes destination number, counted from 1 as
// AlertsHere lists them.
func RemoveAlertHere(number int) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	if number < 1 || number > len(config.Alerts) {
		return fmt.Errorf("there is no alert %d: the server has %d", number, len(config.Alerts))
	}
	config.Alerts = slices.Delete(config.Alerts, number-1, number)
	return SaveConfig(config)
}

// SendTestAlertHere sends a test alert to every destination and says which
// did not take it.
func SendTestAlertHere(ctx context.Context) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	if len(config.Alerts) == 0 {
		return errors.New("the server has no alerts yet: add one with `" + build.Name + " alerts add <server> …`")
	}
	var failed error
	for _, destination := range config.Alerts {
		if err := alert.Send(ctx, destination, testAlert(config)); err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", alertName(destination), err))
		}
	}
	return failed
}

// testAlert is the alert that shows a destination works.
func testAlert(config Config) alert.Message {
	return alert.Message{Server: config.Name, Title: "a test alert from " + config.Name, Text: "Alerts from this server reach you here.", OK: true}
}

// WatchersHere lists the addresses of the watchers the server reports to.
func WatchersHere() ([]string, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	addresses := []string{}
	for _, watcher := range config.Watchers {
		addresses = append(addresses, watcher.URL)
	}
	return addresses, nil
}

// AddWatcherHere has the server report to one more watcher, or gives a
// watcher it has a new token.
func AddWatcherHere(watcher Watcher) error {
	link, err := url.Parse(watcher.URL)
	if err != nil || link.Host == "" || link.Scheme != "https" && link.Scheme != "http" {
		return fmt.Errorf("%q is not the address of a watcher, like https://watcher.example.com", watcher.URL)
	}
	if watcher.Token == "" {
		return errors.New("a watcher needs its token for this server: get it with `" + build.Name + " watcher add <server-name>` on the watcher")
	}
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	config.Watchers = slices.DeleteFunc(config.Watchers, func(w Watcher) bool { return w.URL == watcher.URL })
	config.Watchers = append(config.Watchers, watcher)
	return SaveConfig(config)
}

// RemoveWatcherHere stops the reports to the watcher at address.
func RemoveWatcherHere(address string) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	kept := slices.DeleteFunc(slices.Clone(config.Watchers), func(w Watcher) bool { return w.URL == address })
	if len(kept) == len(config.Watchers) {
		return fmt.Errorf("the server reports to no watcher at %s", address)
	}
	config.Watchers = kept
	return SaveConfig(config)
}
