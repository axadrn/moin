//go:build docker

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestCleanupDocker builds four tiny images and checks what cleanup takes:
// one an old release named, which goes; one a release on disk names, one a
// stopped container uses, and one moin never named, which all stay.
func TestCleanupDocker(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	build := func(tag string) {
		t.Helper()
		context := t.TempDir()
		if err := os.WriteFile(filepath.Join(context, "Dockerfile"), []byte("FROM scratch\nLABEL moin.test.cleanup="+tag+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("docker", "build", "-q", "-t", tag, context).CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v %s", tag, err, out)
		}
		t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", "-f", tag).Run() })
	}
	old, named, inContainer, foreign := "moin-test-cleanup-old:1", "moin-test-cleanup-live:2", "moin-test-cleanup-stopped:1", "moin-test-not-moin:1"
	for _, tag := range []string{old, named, inContainer, foreign} {
		build(tag)
	}
	if out, err := exec.Command("docker", "create", "--name", "moin-test-cleanup-stopped", inContainer, "x").CombinedOutput(); err != nil {
		t.Fatalf("create: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "moin-test-cleanup-stopped").Run() })

	a := newApps(dir, func(protocol.MessageType, string, any) {})
	release := a.releaseDir("shop", 2)
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "compose.json"), []byte(`{"services": {"web": {"image": "`+named+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.writeImages("shop", []string{old, named, inContainer})

	a.cleanup(ctx)

	exists := func(tag string) bool {
		return exec.Command("docker", "image", "inspect", tag).Run() == nil
	}
	if exists(old) {
		t.Errorf("%s, no longer named by a release, is still there", old)
	}
	for _, tag := range []string{named, inContainer, foreign} {
		if !exists(tag) {
			t.Errorf("%s was removed", tag)
		}
	}
	if got := strings.Join(a.rememberedImages("shop"), ","); got != named+","+inContainer {
		t.Errorf("remembered = %s", got)
	}
}
