package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A deploy that fails keeps its log and a record that says so, and the
// list shows it even when the app never ran.
func TestAFailedDeployKeepsItsLogAndRecord(t *testing.T) {
	journal(t)
	t.Setenv("MOIN_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	dir := t.TempDir()
	a := newApps(dir)
	start := a.newStart("shop")
	start.Source = "laptop"
	start.SourceFile = filepath.Join(t.TempDir(), "none.tar.gz")
	var out strings.Builder
	result, err := a.deployHere(context.Background(), start, &out)
	if err == nil {
		t.Fatal("a deploy without a source worked")
	}
	if result.Release != 1 {
		t.Fatalf("the failed deploy says release %d, want 1", result.Release)
	}
	list, err := DeploysHere(dir, "shop")
	if err != nil || len(list) != 1 {
		t.Fatalf("deploys %+v, %v", list, err)
	}
	record := list[0]
	if record.Number != 1 || record.Status != deployFailed || record.Error == "" || record.Source != "laptop" || record.Ended.Before(record.At) || record.Live {
		t.Fatalf("record %+v", record)
	}
	deploy, err := DeployLogHere(dir, "shop", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(deploy.Log, "failed: "+record.Error) {
		t.Fatalf("the log does not say why:\n%s", deploy.Log)
	}
	// The terminal got the lines, but not the error: the CLI says it.
	if strings.Contains(out.String(), "failed: ") {
		t.Fatalf("the terminal got the error line:\n%s", out.String())
	}
	if _, err := DeployLogHere(dir, "shop", 7); err == nil {
		t.Fatal("a deploy that is not there has a log")
	}
	if _, err := DeploysHere(dir, "blog"); !errors.Is(err, ErrNoApp) {
		t.Fatalf("an app with no deploys: %v", err)
	}
}

// A release from before records is listed with "no record": its number
// and the time of its folder.
func TestDeploysListOldReleasesWithoutARecord(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir)
	if err := os.MkdirAll(a.releaseDir("shop", 1), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, a, "shop", DeployRecord{Number: 2, Source: "follow", Status: deployOK, Commit: "3f2a9c1e0b4d5f6a7b8c9d0e1f2a3b4c5d6e7f8a"})
	if err := os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("2"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := DeploysHere(dir, "shop")
	if err != nil || len(list) != 2 {
		t.Fatalf("deploys %+v, %v", list, err)
	}
	if list[0].Number != 2 || !list[0].Live || list[0].What() != "3f2a9c1" {
		t.Errorf("newest %+v", list[0])
	}
	if list[1].Number != 1 || list[1].Status != "" || list[1].At.IsZero() || list[1].Live {
		t.Errorf("old one %+v", list[1])
	}
	deploy, err := DeployLogHere(dir, "shop", 1)
	if err != nil || deploy.Log != "" {
		t.Fatalf("old one's log %q, %v", deploy.Log, err)
	}
}

// A deploy that brings no code, like a rollback or a restart, runs the
// code of the release it starts from: its record says which.
func TestARecordWithoutNewCodeTakesItFromItsRelease(t *testing.T) {
	a := newApps(t.TempDir())
	writeRecord(t, a, "shop", DeployRecord{Number: 2, Commit: "aaaaaaa", Image: "ghcr.io/ada/shop:1.4", ImageID: "9b1c2d3e4f5a"})
	writeRecord(t, a, "shop", DeployRecord{Number: 3, Commit: "bbbbbbb"})
	if err := os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("3"), 0o600); err != nil {
		t.Fatal(err)
	}
	rollback := a.newStart("shop")
	rollback.Source, rollback.FromNumber, rollback.Number = "rollback", 2, 4
	record := a.newRecord(rollback)
	if record.Commit != "aaaaaaa" || record.Image != "ghcr.io/ada/shop:1.4" || record.ImageID != "9b1c2d3e4f5a" || record.From != 2 || record.Status != deployRunning {
		t.Errorf("rollback %+v", record)
	}
	restart := a.newStart("shop")
	if record := a.newRecord(restart); record.Source != "apply" || record.Commit != "bbbbbbb" || record.Image != "" {
		t.Errorf("restart %+v", record)
	}
	// An image the server follows keeps the code of the live release,
	// with its own image.
	image := a.newStart("shop")
	image.ImageName, image.Image = "ghcr.io/ada/shop:main", project("shop")+":0123456789ab"
	if record := a.newRecord(image); record.Commit != "bbbbbbb" || record.Image != "ghcr.io/ada/shop:main" || record.ImageID != "0123456789ab" {
		t.Errorf("image %+v", record)
	}
}

// The source git archive sends carries its commit, and the record reads it.
func TestArchiveCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com"}, args...)...)
		out, err := command.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "first")
	archive := filepath.Join(t.TempDir(), "source.tar.gz")
	git("archive", "--format=tar.gz", "--output="+archive, "HEAD")
	if got, want := archiveCommit(archive), git("rev-parse", "HEAD"); got != want {
		t.Fatalf("commit %q, want %q", got, want)
	}
	plain := filepath.Join(t.TempDir(), "plain.tar.gz")
	if err := os.WriteFile(plain, tarball(t, map[string]string{"main.go": "package main\n"}), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := archiveCommit(plain); got != "" {
		t.Fatalf("a tar without a commit says %q", got)
	}
}

// A long log keeps its end, from the start of a line.
func TestTrimLogKeepsTheEnd(t *testing.T) {
	file := filepath.Join(t.TempDir(), "deploy.log")
	var body strings.Builder
	for i := range 40000 {
		body.WriteString(strings.Repeat("x", 60) + " line " + time.Duration(i).String() + "\n")
	}
	body.WriteString("failed: the last line\n")
	if err := os.WriteFile(file, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	trimLog(file, 1<<20)
	got, _ := os.ReadFile(file)
	text := string(got)
	if len(got) > 1<<20+100 || !strings.HasPrefix(text, "(the lines before are cut") || !strings.HasSuffix(text, "failed: the last line\n") {
		t.Fatalf("trimmed to %d bytes, starts %q", len(got), text[:80])
	}
	if second := strings.SplitN(text, "\n", 3)[1]; !strings.HasPrefix(second, strings.Repeat("x", 60)) {
		t.Fatalf("cut inside a line: %q", second)
	}
}

// A deploy whose command stopped gets its end in its record: live when it
// got that far, else failed.
func TestAStoppedDeployEndsItsRecord(t *testing.T) {
	a := newApps(t.TempDir())
	writeRecord(t, a, "shop", DeployRecord{Number: 1, Status: deployRunning})
	writeRecord(t, a, "shop", DeployRecord{Number: 2, Status: deployRunning})
	a.stopped("shop", 1, true)
	a.stopped("shop", 2, false)
	if record := a.record("shop", 1); record.Status != deployOK || record.Ended.IsZero() {
		t.Errorf("live %+v", record)
	}
	if record := a.record("shop", 2); record.Status != deployFailed || record.Error == "" {
		t.Errorf("stopped %+v", record)
	}
}

// A failed release that prune keeps has only what says why it failed.
func TestKeepRecordOnly(t *testing.T) {
	a := newApps(t.TempDir())
	writeRecord(t, a, "shop", DeployRecord{Number: 3, Status: deployFailed})
	release := a.releaseDir("shop", 3)
	for _, name := range []string{"deploy.log", "kind", "compose.json", "src/main.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(release, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(release, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.keepRecordOnly("shop", 3)
	entries, _ := os.ReadDir(release)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, " ") != "deploy.json deploy.log kind" {
		t.Fatalf("left %v", names)
	}
}

// A deploy the server made on its own says where its log is.
func TestWhyFailedNamesTheLog(t *testing.T) {
	err := whyFailed("shop", Result{Release: 7}, errors.New("build: exit status 1"))
	if err == nil || !strings.Contains(err.Error(), "deploys shop v7`") {
		t.Fatalf("%v", err)
	}
	if err := whyFailed("shop", Result{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := whyFailed("shop", Result{}, io.EOF); err != io.EOF {
		t.Fatalf("a deploy without a number: %v", err)
	}
}

func writeRecord(t *testing.T, a *apps, app string, record DeployRecord) {
	t.Helper()
	if err := writeState(filepath.Join(a.releaseDir(app, record.Number), "deploy.json"), record); err != nil {
		t.Fatal(err)
	}
}
