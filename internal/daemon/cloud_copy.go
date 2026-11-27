package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"filippo.io/age"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
	"github.com/axadrn/moin/protocol"
)

// A copy a hacked server can't delete. The daemon sends each copy it
// takes on its schedule, encrypted as it is, to every watcher that takes
// copies: moin cloud, for a paid workspace. A bucket of your own is not
// needed for it. The daemon can never read such a copy back, so it is
// restored and read here before it leaves (checkDue).
//
//	daemon ── POST <watcher>/api/copies ──▶ nothing in it: do you take copies?
//	daemon ── POST <watcher>/api/copies ──▶ an ID and one address to upload to
//	daemon ── PUT the .age file ──────────▶ that address, 15 minutes long
//	daemon ── POST …/copies/<id>/done ────▶ moin cloud looks it is there
//
// The daemon reads only the status codes and that one address, and goes
// there only over https to a public address. It holds no key to that
// bucket, so it can add a copy but never read, list or delete one.
//
// A restore goes the other way: your laptop gets an address to download
// one copy from and hands it to the server with your key (CloudRestoreHere).

// copyStart is what the daemon tells a watcher of a copy it wants to send.
type copyStart struct {
	App      string    `json:"app"`
	Database string    `json:"database"`
	MadeAt   time.Time `json:"made_at"`
	Size     int64     `json:"size"`
}

// copyUpload is a watcher's answer: the copy's ID and where to put it.
type copyUpload struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// errTakesNoCopies is a watcher that takes no copies, like one you run
// yourself, or moin cloud for a workspace that does not pay.
var errTakesNoCopies = errors.New("takes no copies")

// takingCopies are the watchers that take copies, asked before a copy is
// made, so no database is copied for nobody. The question is a copy with
// nothing in it. A watcher that takes none says so before it looks at it:
// 404 from one you run yourself, 402 from moin cloud for a workspace that
// does not pay. A watcher that gave no answer counts, so the copy says
// what is wrong with it.
func takingCopies(ctx context.Context, watchers []Watcher) []Watcher {
	var taking []Watcher
	for _, watcher := range watchers {
		_, status, err := callWatcher(ctx, watcher, "/api/copies", copyStart{})
		if err == nil && (status == http.StatusNotFound || status == http.StatusPaymentRequired) {
			continue
		}
		taking = append(taking, watcher)
	}
	return taking
}

// toCloud sends the copy id of a database, encrypted as it is in file, to
// each of watchers that takes it. One that took it is enough: only when
// none did, it says why, errNoTaker when none takes copies.
func (b *backups) toCloud(ctx context.Context, app string, database protocol.Database, id string, file *os.File, watchers []Watcher) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	at, _ := protocol.CopyTime(id)
	start := copyStart{App: app, Database: database.Path, MadeAt: at, Size: info.Size()}
	sent, failed := false, error(nil)
	for _, watcher := range watchers {
		upload, err := startCopy(ctx, watcher, start)
		if errors.Is(err, errTakesNoCopies) {
			continue
		}
		if err == nil {
			err = sendCopy(ctx, watcher, upload, file, start.Size)
		}
		if err != nil {
			slog.Warn("cloud copy: the watcher got no copy", "app", app, "copy", id, "error", err)
			failed = err
			continue
		}
		sent = true
		slog.Info("cloud copy: sent", "app", app, "database", database.Path, "copy", id, "size", start.Size)
	}
	switch {
	case sent:
		return nil
	case failed != nil:
		return failed
	}
	return errNoTaker
}

// tempCopy writes body into a new temp file in dir. The caller closes and
// removes it.
func tempCopy(dir string, body io.Reader) (*os.File, error) {
	file, err := os.CreateTemp(dir, ".copy-*.age")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(file, body); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

// startCopy asks a watcher for an address to upload a copy to.
func startCopy(ctx context.Context, watcher Watcher, start copyStart) (copyUpload, error) {
	var upload copyUpload
	answer, status, err := callWatcher(ctx, watcher, "/api/copies", start)
	switch {
	case err != nil:
		return upload, err
	// No such address: a watcher that takes no copies. 402: moin cloud for
	// a workspace that does not pay. 429: a copy came a moment ago.
	case status == http.StatusNotFound || status == http.StatusPaymentRequired || status == http.StatusTooManyRequests:
		return upload, errTakesNoCopies
	case status != http.StatusCreated:
		return upload, fmt.Errorf("the watcher at %s answered HTTP %d: %s", watcher.URL, status, answer)
	}
	if err := json.Unmarshal(answer, &upload); err != nil || upload.ID == "" {
		return upload, fmt.Errorf("the watcher at %s answered no copy", watcher.URL)
	}
	if err := checkCopyURL(upload.URL); err != nil {
		return upload, err
	}
	return upload, nil
}

// sendCopy uploads file to the address a watcher gave, then tells the
// watcher it is done.
func sendCopy(ctx context.Context, watcher Watcher, upload copyUpload, file *os.File, size int64) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, upload.URL, io.NopCloser(file))
	if err != nil {
		return err
	}
	// The address is signed with both: exactly this many bytes, and only
	// where nothing lies yet.
	request.ContentLength = size
	request.Header.Set("If-None-Match", "*")
	response, err := copyClient.Do(request)
	if err != nil {
		return err
	}
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 300))
	response.Body.Close()
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("the upload answered HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(answer)))
	}
	answer, status, err := callWatcher(ctx, watcher, "/api/copies/"+neturl.PathEscape(upload.ID)+"/done", nil)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return fmt.Errorf("the watcher at %s answered HTTP %d: %s", watcher.URL, status, answer)
	}
	return nil
}

// watcherClient asks a watcher about copies. It follows no redirect: a
// redirect is an answer, and the daemon acts on none but the address.
var watcherClient = &http.Client{
	Timeout:       30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// callWatcher posts body as JSON to path of a watcher, with the server's
// token, and returns the answer, short, and its status. It goes only over
// https, or http to this machine or a tailnet, like a report.
func callWatcher(ctx context.Context, watcher Watcher, path string, body any) ([]byte, int, error) {
	parsed, err := neturl.Parse(watcher.URL)
	if err != nil || !(parsed.Scheme == "https" || parsed.Scheme == "http" && report.PlainHTTP(parsed.Hostname())) {
		return nil, 0, fmt.Errorf("the watcher url %q is not https", watcher.URL)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(watcher.URL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+watcher.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", build.Name+"/"+build.Version)
	response, err := watcherClient.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	return bytes.TrimSpace(answer), response.StatusCode, nil
}

// checkCopyURL wants an https address with a name. Where the name points
// is checked when it is dialed (refusePrivate).
func checkCopyURL(address string) error {
	parsed, err := neturl.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("the address of a copy must be https")
	}
	return nil
}

// copyClient uploads and downloads copies. It dials only public
// addresses and follows no redirect, so no address a watcher gives makes
// the server knock on its own network.
var copyClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, Control: refusePrivate}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// errPrivate is an address that is not on the public internet.
var errPrivate = errors.New("the address of a copy is not on the public internet")

// refusePrivate stops a connection to an address that is not public. It
// runs after DNS, so a name that points inside cannot slip past it.
func refusePrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !isPublic(ip.Unmap()) {
		return errPrivate
	}
	return nil
}

// CloudRestore is a restore from a copy in moin cloud: URL downloads it,
// for a few minutes, and Key, your backup key, opens it. Copy names it.
// The key is used for this one restore and written nowhere.
type CloudRestore struct {
	App      string `json:"app"`
	Database string `json:"database"`
	Copy     string `json:"copy"`
	URL      string `json:"url"`
	Key      string `json:"key"`
}

// CloudRestoreHere puts a database of an app back to a copy in moin
// cloud. The copy is downloaded whole first, so the app keeps running
// until it is here; then it is restored like any copy, with an undo.
func CloudRestoreHere(ctx context.Context, dir string, r CloudRestore, out io.Writer) (Restored, error) {
	a := newApps(dir)
	if err := a.mustHave(r.App); err != nil {
		return Restored{}, err
	}
	b := a.backups
	key, err := age.ParseX25519Identity(strings.TrimSpace(r.Key))
	if err != nil {
		return Restored{}, errors.New("this is not a backup key: one starts with AGE-SECRET-KEY-1")
	}
	b.key = key
	database, err := b.one(r.App, r.Database)
	if err != nil {
		return Restored{}, err
	}
	if err := checkCopyURL(r.URL); err != nil {
		return Restored{}, err
	}
	fmt.Fprintf(out, "downloading the copy %s from %s cloud\n", r.Copy, build.Name)
	file, err := downloadCopy(ctx, r.URL, filepath.Dir(a.dir))
	if err != nil {
		return Restored{}, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	fmt.Fprintf(out, "restoring %s to %s\n", database.Path, r.Copy)
	undo, err := b.restoreFile(ctx, r.App, database, file, out)
	if err != nil {
		return Restored{}, err
	}
	return Restored{Database: database.Path, To: r.Copy, Undo: undoPrefix + undo}, nil
}

// downloadCopy downloads a copy into a temp file in dir.
func downloadCopy(ctx context.Context, address, dir string) (*os.File, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := copyClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download the copy: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download the copy: HTTP %d. The address works for 5 minutes: run the restore again", response.StatusCode)
	}
	return tempCopy(dir, response.Body)
}

// restoreFile restores a database from an encrypted copy in file, as
// restore does from the bucket: written down first, after any copy or
// restore of the app, with the state before kept as the undo.
func (b *backups) restoreFile(ctx context.Context, app string, database protocol.Database, file *os.File, out io.Writer) (undo string, err error) {
	unlockRestore, err := b.lockRestore(ctx, app, out)
	if err != nil {
		return "", err
	}
	defer unlockRestore()
	mark := restoreMark{ID: newRestoreID(), App: app, Database: database.Path, Undo: protocol.NewCopyID(time.Now()), State: protocol.RestoreRunning}
	if err := writeState(b.markFile(mark.ID), mark); err != nil {
		return "", fmt.Errorf("write the restore down: %w", err)
	}
	// A deploy that holds the lock ends first.
	unlock := b.apps.lock(app)
	from := source{}
	if _, err = file.Seek(0, io.SeekStart); err == nil {
		from.copy, err = DecryptCopy(file, b.key)
	}
	if err == nil {
		err = b.engine(app, database).Restore(ctx, from, mark.Undo)
	}
	unlock()
	b.finish(mark, err)
	return mark.Undo, err
}
