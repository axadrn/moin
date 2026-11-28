package daemon

import (
	"archive/tar"
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// Every deploy keeps what it printed and a small record of it in the
// folder of its release, for `moin deploys`:
//
//	releases/<n>/deploy.log   its lines as it prints them, the last MB of them
//	releases/<n>/deploy.json  its record: where it came from, what it
//	                          brought, how it ended
//
// A deploy from your laptop, of a branch or an image the server follows,
// of the catalog, a rollback and a restart all keep them. The log is
// written line by line, so a deploy whose SSH connection broke keeps it
// too. A release from before has neither: `moin deploys` says "no record".

// maxDeployLog is how much of a deploy's log stays: the end, where it
// says why it failed.
const maxDeployLog = 1 << 20

// The status of a deploy in its record.
const (
	deployRunning = "running"
	deployOK      = "ok"
	deployFailed  = "failed"
)

// DeployRecord is one deploy of an app: where it came from, what it
// brought and how it ended.
type DeployRecord struct {
	Number int `json:"number"`
	// Source is where the deploy came from: laptop, follow, catalog,
	// rollback or apply (a restart, a start, new env or domains).
	Source string    `json:"source,omitempty"`
	At     time.Time `json:"at"`
	Ended  time.Time `json:"ended,omitzero"`
	// Status is running, ok or failed. It is empty for a release from
	// before deploys kept a record.
	Status string `json:"status"`
	// Error is what a failed deploy failed with.
	Error string `json:"error,omitempty"`
	// Commit is the commit of the code the release runs. Image is the
	// image of its main service as you named it, ImageID the start of
	// Docker's ID of it. Catalog is the catalog app it runs.
	Commit  string `json:"commit,omitempty"`
	Image   string `json:"image,omitempty"`
	ImageID string `json:"image_id,omitempty"`
	Catalog string `json:"catalog,omitempty"`
	// From is the release a rollback ran again.
	From int `json:"from,omitempty"`
	// Copies are the copies of the databases taken just before it.
	Copies []string `json:"copies,omitempty"`
	// Live is the release that runs now. It is not kept in the record:
	// it is said when the deploys are listed.
	Live bool `json:"live,omitempty"`
}

// What is what the deploy runs, short: the catalog app, or the commit and
// the image, like "3f2a9c1" or "my-app:main 9b1c2d3". It is "" for a
// release without a record.
func (r DeployRecord) What() string {
	if r.Catalog != "" {
		return "catalog " + r.Catalog
	}
	parts := []string{}
	if r.Commit != "" {
		parts = append(parts, r.Commit[:min(len(r.Commit), 7)])
	}
	if r.Image != "" {
		parts = append(parts, path.Base(r.Image))
	}
	if r.ImageID != "" {
		parts = append(parts, r.ImageID[:min(len(r.ImageID), 7)])
	}
	return strings.Join(parts, " ")
}

// DeployLog is one deploy with its log.
type DeployLog struct {
	DeployRecord
	Log string `json:"log"`
}

// DeploysHere lists the deploys of app on the server, newest first. An
// app whose first deploy failed has them too: that is when you read why.
func DeploysHere(dir, app string) ([]DeployRecord, error) {
	a := newApps(dir)
	numbers, err := a.deployed(app)
	if err != nil {
		return nil, err
	}
	current := a.current(app)
	slices.Reverse(numbers)
	list := []DeployRecord{}
	for _, number := range numbers {
		record := a.record(app, number)
		record.Live = number == current
		list = append(list, record)
	}
	return list, nil
}

// DeployLogHere is deploy number of app with its log.
func DeployLogHere(dir, app string, number int) (DeployLog, error) {
	a := newApps(dir)
	numbers, err := a.deployed(app)
	if err != nil {
		return DeployLog{}, err
	}
	if !slices.Contains(numbers, number) {
		return DeployLog{}, fmt.Errorf("v%d of %s is not on the server: see them with `%s deploys %s`", number, app, build.Name, app)
	}
	deploy := DeployLog{DeployRecord: a.record(app, number)}
	deploy.Live = number == a.current(app)
	body, err := os.ReadFile(filepath.Join(a.releaseDir(app, number), "deploy.log"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return DeployLog{}, err
	}
	deploy.Log = string(body)
	return deploy, nil
}

// deployed are the numbers of the app's releases, oldest first. It fails
// for an app with none on this server.
func (a *apps) deployed(app string) ([]int, error) {
	if !protocol.ValidAppName(app) {
		return nil, fmt.Errorf("%q is not a valid app name: lowercase letters, digits and dashes", app)
	}
	numbers := a.releases(app)
	if len(numbers) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoApp, app)
	}
	return numbers, nil
}

// record is the record of release number of app. A release from before
// records has only its number and the time of its folder.
func (a *apps) record(app string, number int) DeployRecord {
	dir := a.releaseDir(app, number)
	var record DeployRecord
	body, err := os.ReadFile(filepath.Join(dir, "deploy.json"))
	if err == nil && json.Unmarshal(body, &record) == nil {
		record.Number = number
		return record
	}
	record = DeployRecord{Number: number}
	if info, err := os.Stat(dir); err == nil {
		record.At = info.ModTime()
	}
	return record
}

// recorder writes the log and the record of the deploy that runs. logf
// writes a line to out, the terminal of whoever deploys, and to the log.
type recorder struct {
	DeployRecord
	// file is the record's deploy.json.
	file string
	log  *os.File
	out  func(string, ...any)
}

func (r *recorder) logf(format string, args ...any) {
	r.out(format, args...)
	fmt.Fprintf(r.log, format+"\n", args...)
}

// startRecord makes the empty folder of the release, with its log and
// its record, before any step of the deploy writes a line.
func (a *apps) startRecord(start protocol.DeployStart, out func(string, ...any)) (*recorder, error) {
	dir := a.releaseDir(start.App, start.Number)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(dir, "deploy.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	r := &recorder{DeployRecord: a.newRecord(start), file: filepath.Join(dir, "deploy.json"), log: log, out: out}
	if err := writeState(r.file, r.DeployRecord); err != nil {
		log.Close()
		return nil, err
	}
	return r, nil
}

// newRecord is the record of a deploy as it starts. A deploy that brings
// no code runs the code of the release it starts from: for a rollback
// that release, else the live one.
func (a *apps) newRecord(start protocol.DeployStart) DeployRecord {
	record := DeployRecord{
		Number:  start.Number,
		Source:  start.Source,
		At:      time.Now(),
		Status:  deployRunning,
		Catalog: start.Catalog,
		From:    start.FromNumber,
		Image:   start.ImageName,
	}
	if start.ImageName != "" {
		record.ImageID = strings.TrimPrefix(start.Image, project(start.App)+":")
	}
	switch {
	case start.SourceFile != "":
		record.Commit = archiveCommit(start.SourceFile)
	case start.Catalog == "":
		base := a.record(start.App, cmp.Or(start.FromNumber, a.current(start.App)))
		record.Commit, record.Catalog = base.Commit, base.Catalog
		if record.Image == "" {
			record.Image, record.ImageID = base.Image, base.ImageID
		}
	}
	return record
}

// end writes how the deploy ended into its log and its record, and keeps
// the last MB of the log.
func (r *recorder) end(err error) {
	r.Ended = time.Now()
	r.Status = deployOK
	if err != nil {
		r.Status, r.Error = deployFailed, err.Error()
		// The terminal shows the error on its own: only the log needs it.
		fmt.Fprintf(r.log, "failed: %s\n", err)
	}
	r.log.Close()
	trimLog(r.log.Name(), maxDeployLog)
	if err := writeState(r.file, r.DeployRecord); err != nil {
		slog.Warn("write the deploy's record", "file", r.file, "error", err)
	}
}

// stopped writes into the record of a deploy whose command stopped how it
// ended, as endDeploy found it: live, or failed.
func (a *apps) stopped(app string, number int, live bool) {
	record := a.record(app, number)
	if record.Status != deployRunning {
		return
	}
	record.Ended = time.Now()
	record.Status = deployOK
	if !live {
		record.Status, record.Error = deployFailed, "the deploy stopped before it was live"
	}
	if err := writeState(filepath.Join(a.releaseDir(app, number), "deploy.json"), record); err != nil {
		slog.Warn("write the deploy's record", "app", app, "number", number, "error", err)
	}
}

// trimLog keeps the last max bytes of the log at path, from the start of
// a line.
func trimLog(path string, max int64) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= max {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	body := make([]byte, max)
	_, err = file.ReadAt(body, info.Size()-max)
	file.Close()
	if err != nil {
		return
	}
	if line := bytes.IndexByte(body, '\n'); line >= 0 {
		body = body[line+1:]
	}
	cut := fmt.Appendf(nil, "(the lines before are cut: the log keeps its last %d MB)\n", max>>20)
	if err := writeWhole(path, append(cut, body...)); err != nil {
		slog.Warn("cut the deploy log", "file", path, "error", err)
	}
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// archiveCommit is the commit a tar.gz of git archive was made from, ""
// for one without. git writes it into a pax header at the start, where
// `git get-tar-commit-id` reads it too.
func archiveCommit(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	archive, err := gzip.NewReader(file)
	if err != nil {
		return ""
	}
	header, err := tar.NewReader(archive).Next()
	if err != nil || header.Typeflag != tar.TypeXGlobalHeader {
		return ""
	}
	commit := header.PAXRecords["comment"]
	if !commitPattern.MatchString(commit) {
		return ""
	}
	return commit
}

// keepRecordOnly removes all of a failed release but what says why it
// failed: its log, its record and its kind.
func (a *apps) keepRecordOnly(app string, number int) {
	dir := a.releaseDir(app, number)
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		switch entry.Name() {
		case "deploy.log", "deploy.json", "kind":
		default:
			_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
		}
	}
}
