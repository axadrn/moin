package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestUnusedImages: only an image moin named, that no release on disk
// names and no container runs, goes; an image Docker no longer has is
// forgotten; names compare the way docker image ls writes them.
func TestUnusedImages(t *testing.T) {
	remembered := []string{"postgres:16", "postgres:17", "ghcr.io/me/shop:3f2a", "ghcr.io/me/shop:9c1d", "redis", "moin-shop-web:4", "gone:1"}
	named := []string{"postgres:17", "moin-shop-web:4"}
	inContainers := []string{"ghcr.io/me/shop:9c1d", "docker.io/library/nginx:1.29-alpine"}
	present := []string{"postgres:16", "postgres:17", "ghcr.io/me/shop:3f2a", "ghcr.io/me/shop:9c1d", "redis:latest", "moin-shop-web:4", "nginx:1.29-alpine", "my-own-tool:1"}
	remove, keep := unusedImages(remembered, named, inContainers, present)
	if want := []string{"postgres:16", "ghcr.io/me/shop:3f2a", "redis:latest"}; !reflect.DeepEqual(remove, want) {
		t.Errorf("remove = %v, want %v", remove, want)
	}
	if want := []string{"postgres:17", "ghcr.io/me/shop:9c1d", "moin-shop-web:4"}; !reflect.DeepEqual(keep, want) {
		t.Errorf("keep = %v, want %v", keep, want)
	}
}

// TestRememberImages: the images file grows with the images of the
// releases, so the names survive prune.
func TestRememberImages(t *testing.T) {
	a := newApps(t.TempDir(), func(protocol.MessageType, string, any) {})
	release := a.releaseDir("shop", 1)
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "compose.json"), []byte(`{"services": {"db": {"image": "postgres:16"}, "web": {"image": "moin-shop-web:1"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.rememberImages("shop")
	if err := os.RemoveAll(release); err != nil {
		t.Fatal(err)
	}
	a.rememberImages("shop")
	if got, want := a.rememberedImages("shop"), []string{"moin-shop-web:1", "postgres:16"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remembered = %v, want %v", got, want)
	}
}

// TestReclaimed reads what docker's prunes say they freed.
func TestReclaimed(t *testing.T) {
	for out, want := range map[string]int64{
		"Deleted Images:\ndeleted: sha256:abc\n\nTotal reclaimed space: 1.5GB\n": 1_500_000_000,
		"ID\tRECLAIMABLE\tSIZE\nx1\ttrue\t12MB\nTotal:\t340MB\n":                 340_000_000,
		"Total reclaimed space: 0B\n":                                            0,
		"nothing":                                                                0,
	} {
		if got := reclaimed(out); got != want {
			t.Errorf("reclaimed(%q) = %d, want %d", out, got, want)
		}
	}
	if got := sizeText(2_100_000_000); got != "2.1 GB" {
		t.Errorf("sizeText = %q", got)
	}
}
