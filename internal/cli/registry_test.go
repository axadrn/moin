package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBringImage: an image on this computer is what gets deployed. The
// server pulls it only when its registry has the very same digest, else
// the image is sent.
func TestBringImage(t *testing.T) {
	cases := []struct {
		name, here, want string
	}{
		{"the registry has the same", "ghcr.io/ada/shop@sha256:aaa", "'registry' 'pull'"},
		{"the registry has another", "ghcr.io/ada/shop@sha256:bbb", "'docker' 'load'"},
		{"never pushed", "", "'docker' 'load'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runs := fakeSSH(t, `{"image": "ghcr.io/ada/shop:main", "digest": "sha256:aaa"}`)
			docker := "#!/bin/sh\n[ \"$2\" = inspect ] && echo '" + c.here + "'\ntrue\n"
			if err := os.WriteFile(filepath.Join(filepath.Dir(runs), "docker"), []byte(docker), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := bringImage(context.Background(), "prod", "ghcr.io/ada/shop:main", io.Discard); err != nil {
				t.Fatal(err)
			}
			args, _ := os.ReadFile(runs)
			if !strings.Contains(string(args), c.want) {
				t.Errorf("ssh ran %q, want %q", args, c.want)
			}
		})
	}
}
