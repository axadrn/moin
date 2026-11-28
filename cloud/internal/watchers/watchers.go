// Package watchers is the watcher moin cloud runs: one watcher of
// internal/watcher per workspace, all fed by POST /report, all visiting
// domains through one Visitor with a cap. Alerts go to the workspace's
// mail and where each server's report says. What a watcher knows holds
// those destinations, so it is saved encrypted.
package watchers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/cloud/internal/counts"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
	"github.com/axadrn/moin/internal/watcher"
)

const (
	// tickEvery is how often every watcher looks for silent servers.
	tickEvery = 5 * time.Second
	// visitEvery is how often every domain is visited.
	visitEvery = time.Minute
	// visitsAtOnce is how many visits run at the same time, for everyone.
	visitsAtOnce = 32
)

// ErrUnknownToken is a report with a token no server has.
var ErrUnknownToken = errors.New("unknown token")

// mailKind is the one alert kind only moin cloud has: a mail to the
// workspace.
const mailKind = "mail"

// Watchers are the watchers of every workspace with a server.
type Watchers struct {
	store   *workspace.Store
	counts  *counts.Counts
	mailer  mailer.Mailer
	visitor *watcher.Visitor
	appURL  string
	// sealer encrypts what a watcher knows before it is saved: AES-GCM.
	sealer cipher.AEAD

	mu sync.Mutex
	// byWorkspace are the watchers by workspace id, made when first needed.
	byWorkspace map[string]*watcher.Watcher
}

// New are the watchers of the workspaces in store. Each report adds to
// counts. Alerts go out through mailer, with a link to appURL. What each
// watcher knows is saved encrypted with key.
func New(store *workspace.Store, counts *counts.Counts, mailer mailer.Mailer, appURL string, key [32]byte) *Watchers {
	// A key of 32 bytes is always one AES and GCM take.
	block, _ := aes.NewCipher(key[:])
	sealer, _ := cipher.NewGCM(block)
	return &Watchers{
		store:       store,
		counts:      counts,
		mailer:      mailer,
		visitor:     watcher.NewVisitor(visitsAtOnce),
		appURL:      strings.TrimRight(appURL, "/"),
		sealer:      sealer,
		byWorkspace: map[string]*watcher.Watcher{},
	}
}

// seal encrypts what the watcher of workspace id knows: a fresh nonce,
// then the ciphertext. The id is sealed in, so one workspace's state
// never opens as another's.
func (ws *Watchers) seal(id string, state []byte) []byte {
	nonce := make([]byte, ws.sealer.NonceSize())
	rand.Read(nonce)
	return ws.sealer.Seal(nonce, nonce, state, []byte(id))
}

// open decrypts what seal made. Nothing saved stays nothing.
func (ws *Watchers) open(id string, sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	size := ws.sealer.NonceSize()
	if len(sealed) < size {
		return nil, errors.New("the saved watcher state is too short")
	}
	state, err := ws.sealer.Open(nil, sealed[:size], sealed[size:], []byte(id))
	if err != nil {
		return nil, errors.New("the saved watcher state does not open with SECRETS_KEY")
	}
	return state, nil
}

// Counts are the counts the reports add to, for the landing page.
func (ws *Watchers) Counts() *counts.Counts {
	return ws.counts
}

// Load makes the watcher of every workspace with a server, so silence is
// noticed after a restart too.
func (ws *Watchers) Load() error {
	ids, err := ws.store.WatchedWorkspaces()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := ws.of(id); err != nil {
			return err
		}
	}
	return nil
}

// of is the watcher of a workspace, made from what it saved the first
// time it is needed.
func (ws *Watchers) of(id string) (*watcher.Watcher, error) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if w := ws.byWorkspace[id]; w != nil {
		return w, nil
	}
	space, err := ws.store.Workspace(id)
	if err != nil {
		return nil, err
	}
	servers, err := ws.store.ServerNames(id)
	if err != nil {
		return nil, err
	}
	sealed, err := ws.store.WatcherState(id)
	if err != nil {
		return nil, err
	}
	// A state that does not open, like one saved before states were
	// encrypted, is dropped: the next reports build it again.
	saved, err := ws.open(id, sealed)
	if err != nil {
		slog.Warn("starting the watcher with nothing saved", "workspace", id, "error", err)
	}
	w, err := watcher.New(servers, []alert.Destination{{Kind: mailKind}}, saved)
	if err != nil {
		return nil, fmt.Errorf("workspace %s: %w", id, err)
	}
	w.Send = ws.mail(id, space.Mail)
	w.SendReported = ws.reported(id)
	w.Connect = func(server string) string { return build.Name + " watcher connect " + server }
	ws.byWorkspace[id] = w
	return w, nil
}

// mail sends an alert as a mail to the workspace.
func (ws *Watchers) mail(id, to string) func(context.Context, alert.Destination, alert.Message) error {
	return func(ctx context.Context, _ alert.Destination, m alert.Message) error {
		if !ws.alertsOn(id, m) {
			return nil
		}
		text := m.Text
		if text != "" {
			text += "\n\n"
		}
		return ws.mailer.Send(ctx, mailer.Message{To: to, Subject: m.Title, Text: text + ws.appURL + "/cloud"})
	}
}

// reported sends an alert to a destination a server's report named, as
// every watcher does: over https, to a public address.
func (ws *Watchers) reported(id string) func(context.Context, alert.Destination, alert.Message) error {
	return func(ctx context.Context, d alert.Destination, m alert.Message) error {
		if !ws.alertsOn(id, m) {
			return nil
		}
		return watcher.SendPublic(ctx, d, m)
	}
}

// alertsOn says whether the alerts of a workspace go out. After the
// trial, unpaid, none does: it is dropped, not kept for later.
func (ws *Watchers) alertsOn(id string, m alert.Message) bool {
	space, err := ws.store.Workspace(id)
	if err != nil {
		slog.Error("alert not sent", "workspace", id, "title", m.Title, "error", err)
		return false
	}
	if !space.AlertsOn(time.Now()) {
		slog.Info("alert not sent, the trial is over", "workspace", id, "title", m.Title)
		return false
	}
	return true
}

// Receive takes the report of the server whose token it carries. The
// first report starts the trial.
func (ws *Watchers) Receive(token string, r report.Report) error {
	id, name, err := ws.store.ServerByToken(token)
	if errors.Is(err, workspace.ErrNotFound) {
		return ErrUnknownToken
	}
	if err != nil {
		return err
	}
	w, err := ws.of(id)
	if err != nil {
		return err
	}
	if err := w.Receive(name, r); err != nil {
		return err
	}
	if err := ws.counts.Report(id, name, r, time.Now()); err != nil {
		slog.Error("count a report", "error", err)
	}
	return ws.store.StartTrial(id, time.Now())
}

// Changed tells the watcher of a workspace that its servers changed.
func (ws *Watchers) Changed(id string) error {
	w, err := ws.of(id)
	if err != nil {
		return err
	}
	servers, err := ws.store.ServerNames(id)
	if err != nil {
		return err
	}
	w.SetServers(servers)
	return nil
}

// Forget drops the watcher of a workspace that is gone.
func (ws *Watchers) Forget(id string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.byWorkspace, id)
}

// View is what the watcher of a workspace shows.
func (ws *Watchers) View(id string) (watcher.View, error) {
	w, err := ws.of(id)
	if err != nil {
		return watcher.View{}, err
	}
	return w.View(), nil
}

// Run ticks every watcher every few seconds and visits every domain every
// minute, and saves what changed, until ctx ends.
func (ws *Watchers) Run(ctx context.Context) {
	ticks := time.NewTicker(tickEvery)
	visits := time.NewTicker(visitEvery)
	defer ticks.Stop()
	defer visits.Stop()
	for {
		select {
		case <-ctx.Done():
			ws.save()
			return
		case <-ticks.C:
			ws.tick(ctx)
		case <-visits.C:
			ws.visit(ctx)
			ws.tick(ctx)
			ws.mailTrials(ctx)
		}
		ws.save()
	}
}

// all are the watchers there are now, with their workspace ids.
func (ws *Watchers) all() map[string]*watcher.Watcher {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	all := make(map[string]*watcher.Watcher, len(ws.byWorkspace))
	for id, w := range ws.byWorkspace {
		all[id] = w
	}
	return all
}

// tick ticks every watcher at the same time: a workspace whose alerts go
// out slowly only makes itself late.
func (ws *Watchers) tick(ctx context.Context) {
	var wait sync.WaitGroup
	for _, w := range ws.all() {
		wait.Go(func() { w.Tick(ctx) })
	}
	wait.Wait()
}

// visit visits the domains of every watcher at once; the Visitor keeps
// the number of visits at its cap.
func (ws *Watchers) visit(ctx context.Context) {
	var wait sync.WaitGroup
	for _, w := range ws.all() {
		wait.Go(func() { w.Visit(ctx, ws.visitor) })
	}
	wait.Wait()
}

// save keeps what every watcher knows, when it changed.
func (ws *Watchers) save() {
	for id, w := range ws.all() {
		err := w.Save(func(state []byte) error { return ws.store.SaveWatcherState(id, ws.seal(id, state)) })
		if err != nil {
			slog.Error("save watcher state", "workspace", id, "error", err)
		}
	}
}
