package daemon

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The logins are read from Docker's own config.json: the host, the user,
// and docker.io for Docker Hub. The token is never answered.
func TestRegistriesHere(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	list, err := RegistriesHere()
	if err != nil || len(list) != 0 {
		t.Fatalf("without a config.json: %v, %v", list, err)
	}
	auth := func(login string) string { return base64.StdEncoding.EncodeToString([]byte(login)) }
	config := `{"auths": {"ghcr.io": {"auth": "` + auth("ada:a-token") + `"}, "https://index.docker.io/v1/": {"auth": "` + auth("bob:hub-token") + `"}}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err = RegistriesHere()
	want := []Registry{{Registry: "docker.io", User: "bob"}, {Registry: "ghcr.io", User: "ada"}}
	if err != nil || len(list) != 2 || list[0] != want[0] || list[1] != want[1] {
		t.Fatalf("got %+v, %v, want %+v", list, err, want)
	}
}

// A registry is a host, never an option of docker or a path.
func TestSetRegistryRefusesWhatIsNoRegistry(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	for _, registry := range []string{"", "--config=/tmp", "ghcr.io/ada", "ghcr.io x"} {
		err := SetRegistryHere(t.Context(), Registry{Registry: registry, User: "ada", Token: "secret"})
		if err == nil || !strings.Contains(err.Error(), "is not a registry") {
			t.Errorf("%q: %v", registry, err)
		}
	}
	if err := SetRegistryHere(t.Context(), Registry{Registry: "ghcr.io", User: "ada"}); err == nil {
		t.Error("a login without a token was taken")
	}
	if err := RemoveRegistryHere(t.Context(), "ghcr.io"); err == nil || !strings.Contains(err.Error(), "no login for ghcr.io") {
		t.Errorf("removing a login that is not there: %v", err)
	}
}

func TestValidImage(t *testing.T) {
	for image, want := range map[string]bool{"shop:dev": true, "ghcr.io/ada/shop:1.4": true, "ghcr.io/ada/shop@sha256:3f2a": true, "": false, "--platform=x": false, "a b": false} {
		if validImage(image) != want {
			t.Errorf("validImage(%q) = %v", image, !want)
		}
	}
}

// A new image under the tag deploys once, the same image again does
// nothing, and a deploy that fails alerts with the image's short ID.
func TestFollowDeploysEachNewImageOnce(t *testing.T) {
	first, second := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	fake := &fakeFollower{head: first}
	fw := fake.follower()
	f := fw.look(t.Context(), Follow{App: "shop", Image: "ghcr.io/ada/shop:main"})
	f = fw.look(t.Context(), f)
	if f.ImageID != first || f.Commit != "" || len(fake.deployed) != 1 || len(fake.alerts) != 0 {
		t.Fatalf("after two looks at one image: %+v, deployed %q, alerts %v", f, fake.deployed, fake.alerts)
	}
	fake.head, fake.fail = second, errors.New("the app did not start")
	f = fw.look(t.Context(), f)
	f = fw.look(t.Context(), f)
	if f.ImageID != second || len(fake.deployed) != 2 || len(fake.alerts) != 1 || fake.alerts[0].Title != "shop: the deploy of bbbbbbbbbbbb from ghcr.io/ada/shop:main failed" {
		t.Fatalf("a new image that fails: %+v, deployed %q, alerts %+v", f, fake.deployed, fake.alerts)
	}
}

// An image moin cannot pull alerts once while it lasts.
func TestFollowAlertsAnImageItCannotPullOnce(t *testing.T) {
	fake := &fakeFollower{readFail: errors.New("unauthorized")}
	fw := fake.follower()
	f := Follow{App: "shop", Image: "ghcr.io/ada/shop:main"}
	for range 3 {
		f = fw.look(t.Context(), f)
	}
	alerts := fake.alerts
	if len(fake.deployed) != 0 || len(alerts) != 1 || !strings.HasSuffix(alerts[0].Title, "cannot read the image ghcr.io/ada/shop:main") {
		t.Fatalf("deployed %q, alerts %+v", fake.deployed, alerts)
	}
}

// Following an image replaces the branch the app followed: its deploy
// key goes. Following a branch again forgets the image.
func TestFollowImageReplacesTheBranch(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	dir := t.TempDir()
	a := newApps(dir)
	if err := os.MkdirAll(a.appDir("shop"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := FollowHere(dir, "shop", "git@example.com:ada/shop.git", "main"); err != nil {
		t.Fatal(err)
	}
	f, err := FollowImageHere(dir, "shop", "ghcr.io/ada/shop:main")
	if err != nil || f.Image != "ghcr.io/ada/shop:main" || f.Branch != "" || exists(filepath.Join(followDir(dir, "shop"), "key")) {
		t.Fatalf("follow the image: %+v, %v", f, err)
	}
	if _, err := FollowImageHere(dir, "shop", "--all-tags"); err == nil {
		t.Fatal("an option was taken as an image")
	}
	f, err = FollowHere(dir, "shop", "git@example.com:ada/shop.git", "main")
	if err != nil || f.Image != "" || f.Branch != "main" {
		t.Fatalf("follow the branch again: %+v, %v", f, err)
	}
}
