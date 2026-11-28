// Package copies keeps the copy a hacked server can add to but never
// delete. After each backup a server asks for an address to upload one
// encrypted copy to, uploads it, and says it is done:
//
//	server ── POST /api/copies ──────────▶ moin cloud: plan, storage, rate
//	server ◀─ an address for one upload ── valid 15 minutes
//	server ── PUT the .age file ─────────▶ the bucket, locked 14 days
//	server ── POST /api/copies/<id>/done ▶ moin cloud looks it is there
//
// The server holds no key to the bucket. It can't list, read, delete or
// overwrite a copy. Old copies go here, in Prune, never on the server.
package copies

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

const (
	// Storage is how many bytes of copies a workspace keeps: fair use.
	Storage = 50 << 30
	// Keep is how long a copy stays, as long as the bucket's lock.
	Keep = 14 * 24 * time.Hour
	// Grace is how long the copies stay after a subscription or a trial
	// ended.
	Grace = 30 * 24 * time.Hour
	// every is how far apart the copies of one database are made, by the
	// time the server took them: one an hour, with ten minutes to spare
	// for a dump or a check that took long.
	every = 50 * time.Minute
	// perHour is how many copies a whole workspace may send in an hour.
	perHour = 100
)

var (
	ErrUnknownToken = errors.New("unknown token")
	ErrNotPaid      = errors.New("copies come with a paid plan of " + build.Name + " cloud")
	ErrFull         = fmt.Errorf("the storage for copies is full, %d GB: old copies go after 14 days, then new ones fit", Storage>>30)
	ErrTooOften     = errors.New("another copy of this database was made within 50 minutes: one an hour per database")
	ErrNotUploaded  = errors.New("the copy is not in the bucket: upload it first")
	ErrNotFound     = errors.New("no such copy")
	ErrNotACopy     = errors.New("not a copy")
)

// Copies are the copies of every workspace.
type Copies struct {
	store  *workspace.Store
	bucket *Bucket
	mailer mailer.Mailer
	appURL string
	// mu lets one Start at a time count and add, so requests at once
	// can't all pass the limits before any of them is counted.
	mu sync.Mutex
}

// New keeps the copies of the workspaces in store in bucket.
func New(store *workspace.Store, bucket *Bucket, mailer mailer.Mailer, appURL string) *Copies {
	return &Copies{store: store, bucket: bucket, mailer: mailer, appURL: strings.TrimRight(appURL, "/")}
}

// Request is what a server says of the copy it wants to upload.
type Request struct {
	App      string    `json:"app"`
	Database string    `json:"database"`
	MadeAt   time.Time `json:"made_at"`
	Size     int64     `json:"size"`
}

// Upload is the answer to a server: its copy's ID and where to put it.
type Upload struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// Start gives the server of token an address to upload one copy to,
// when its workspace pays or its trial runs, has room for it, and did not send too many.
func (c *Copies) Start(ctx context.Context, token string, r Request) (Upload, error) {
	space, server, err := c.server(token)
	if err != nil {
		return Upload{}, err
	}
	now := time.Now()
	if !space.CopiesOn(now) {
		return Upload{}, ErrNotPaid
	}
	if err := r.check(now); err != nil {
		return Upload{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	near, err := c.store.CopiesMadeBetween(space.ID, server, r.App, r.Database, r.MadeAt.Add(-every), r.MadeAt.Add(every))
	if err != nil {
		return Upload{}, err
	}
	all, err := c.store.CopiesSince(space.ID, now.Add(-time.Hour))
	if err != nil {
		return Upload{}, err
	}
	if near > 0 || all >= perHour {
		return Upload{}, ErrTooOften
	}
	used, err := c.store.Storage(space.ID)
	if err != nil {
		return Upload{}, err
	}
	if used+r.Size > Storage {
		c.mailFull(ctx, space, now)
		return Upload{}, ErrFull
	}
	copy, err := c.store.NewCopy(workspace.Copy{WorkspaceID: space.ID, Server: server, App: r.App, Database: r.Database, MadeAt: r.MadeAt, Size: r.Size})
	if err != nil {
		return Upload{}, err
	}
	address, err := c.bucket.UploadURL(ctx, copy.Key(), r.Size)
	if err != nil {
		return Upload{}, err
	}
	slog.Info("copy: upload starts", "workspace", space.ID, "server", server, "app", r.App, "copy", copy.ID, "size", r.Size)
	return Upload{ID: copy.ID, URL: address}, nil
}

// check refuses a request that is no copy of a database. A copy made
// more than an hour after now is from a wrong clock or a lie: it would
// pass as the newest.
func (r Request) check(now time.Time) error {
	switch {
	case !protocol.ValidAppName(r.App):
		return fmt.Errorf("%w: %q is not an app", ErrNotACopy, r.App)
	case r.Database == "" || len(r.Database) > 200 || strings.ContainsFunc(r.Database, unicode.IsControl):
		return fmt.Errorf("%w: %q is not a database", ErrNotACopy, r.Database)
	case r.MadeAt.IsZero():
		return fmt.Errorf("%w: it needs the time it was made", ErrNotACopy)
	case r.MadeAt.After(now.Add(time.Hour)):
		return fmt.Errorf("%w: it was made at %s, in the future", ErrNotACopy, r.MadeAt.UTC().Format(time.RFC3339))
	case r.Size <= 0 || r.Size > Storage:
		return fmt.Errorf("%w: %d bytes do not fit", ErrNotACopy, r.Size)
	}
	return nil
}

// Done notes that the server of token uploaded the copy id: moin cloud
// looks that it is in the bucket and keeps its size there.
func (c *Copies) Done(ctx context.Context, token, id string) error {
	space, server, err := c.server(token)
	if err != nil {
		return err
	}
	copy, err := c.store.CopyOf(space.ID, id)
	if errors.Is(err, workspace.ErrNotFound) || err == nil && copy.Server != server {
		return ErrNotFound
	}
	if err != nil || copy.State == "done" {
		return err
	}
	size, err := c.bucket.Size(ctx, copy.Key())
	if errors.Is(err, errMissing) {
		return ErrNotUploaded
	}
	if err != nil {
		return err
	}
	slog.Info("copy: done", "workspace", space.ID, "server", server, "copy", id, "size", size)
	return c.store.CopyDone(id, size)
}

// server is the workspace and the name of the server a token belongs to.
func (c *Copies) server(token string) (workspace.Workspace, string, error) {
	id, server, err := c.store.ServerByToken(token)
	if errors.Is(err, workspace.ErrNotFound) {
		return workspace.Workspace{}, "", ErrUnknownToken
	}
	if err != nil {
		return workspace.Workspace{}, "", err
	}
	space, err := c.store.Workspace(id)
	return space, server, err
}

// List are the copies of a workspace, newest first: of one server and
// one app, or of all when they are "".
func (c *Copies) List(workspaceID, server, app string) ([]workspace.Copy, error) {
	return c.store.Copies(workspaceID, server, app)
}

// DownloadURL is an address that downloads the copy id of a workspace,
// for five minutes. The copy is encrypted: only your key opens it.
func (c *Copies) DownloadURL(ctx context.Context, workspaceID, id string) (string, error) {
	copy, err := c.store.CopyOf(workspaceID, id)
	if errors.Is(err, workspace.ErrNotFound) || err == nil && copy.State != "done" {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return c.bucket.DownloadURL(ctx, copy.Key())
}

// Prune deletes the copies whose time is over: older than Keep, but
// never the newest of a database; uploads that never ended; and every
// copy of a workspace that ended, or whose trial ran out, Grace ago, or
// was deleted. A copy the
// lock still keeps goes on a later round.
func (c *Copies) Prune(ctx context.Context, now time.Time) error {
	due, err := c.store.Due(now, Keep, Grace)
	if err != nil {
		return err
	}
	for _, copy := range due {
		err := c.bucket.Delete(ctx, copy.Key())
		if errors.Is(err, errLocked) {
			continue
		}
		if err != nil {
			return fmt.Errorf("delete the copy %s: %w", copy.ID, err)
		}
		if err := c.store.DeleteCopy(copy.ID); err != nil {
			return err
		}
	}
	return nil
}

// Run prunes every hour, until ctx ends.
func (c *Copies) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := c.Prune(ctx, time.Now()); err != nil {
			slog.Error("copy: prune", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// mailFull tells a workspace its storage is full, once a day at most.
func (c *Copies) mailFull(ctx context.Context, space workspace.Workspace, now time.Time) {
	due, err := c.store.StorageMailDue(space.ID, now)
	if err != nil || !due {
		return
	}
	message := mailer.Message{
		To:      space.Mail,
		Subject: "Your " + build.Name + " cloud storage for copies is full",
		Text: fmt.Sprintf("Your servers sent %d GB of copies to %s cloud. That is all it keeps for a workspace. "+
			"No new copy goes in until old ones end, 14 days after they came. No old copy is deleted for it.\n\n"+
			"Your servers still copy to your own bucket.\n\nSee the copies: %s backups <app>\n%s/cloud",
			Storage>>30, build.Name, build.Name, c.appURL),
	}
	if err := c.mailer.Send(ctx, message); err != nil {
		slog.Error("mail not sent", "subject", message.Subject, "error", err)
	}
}
