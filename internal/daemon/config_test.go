package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/protocol"
)

// useConfig points the config file at a temp file with body.
func useConfig(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.yml")
	if body != "" {
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MOIN_CONFIG", file)
	return file
}

// The example in the comment of DefaultConfigFile reads as it is written.
func TestLoadConfig(t *testing.T) {
	useConfig(t, `
name: prod
watchers:
  - url: https://watcher.example.com
    token: t1
alerts:
  - kind: telegram
    token: bot
    chat_id: "42"
bucket:
  endpoint: https://s3.example.com
  region: auto
  bucket: backups
  path: moin
  access_key: key
  secret_key: secret
backups:
  every: 1d
  keep: 30d
  apps:
    shop:
      every: 15m
mounts: [/mnt/media]
`)
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Name != "prod" || config.Watchers[0].Token != "t1" || config.Alerts[0].ChatID != "42" || config.Bucket.SecretKey != "secret" || config.Mounts[0] != "/mnt/media" {
		t.Fatalf("config = %+v", config)
	}
	// An app takes what it sets and the server's schedule for the rest.
	if every, keep := config.schedule("shop"); every != 15*time.Minute || keep != 30*day {
		t.Errorf("shop: every %s, keep %s", every, keep)
	}
	if every, keep := config.schedule("blog"); every != day || keep != 30*day {
		t.Errorf("blog: every %s, keep %s", every, keep)
	}
}

// A server without a config file has its hostname as name, no bucket,
// and copies hourly, kept a week, once it has one.
func TestConfigDefaults(t *testing.T) {
	useConfig(t, "")
	config, err := LoadConfig()
	hostname, _ := os.Hostname()
	if err != nil || config.Name != hostname || config.Bucket != nil {
		t.Fatalf("config = %+v, %v", config, err)
	}
	if every, keep := config.schedule("shop"); every != time.Hour || keep != 7*day {
		t.Errorf("every %s, keep %s", every, keep)
	}
}

// A typo in the config is an error that names it: it never changes how
// long copies are kept without a word.
func TestConfigMistakes(t *testing.T) {
	for body, want := range map[string]string{
		"backups: {keep: 7}":                          `"7" is not a time span`,
		"backups: {every: 1w}":                        `"1w" is not a time span`,
		"backups: {every: 1m}":                        "too often",
		"backups: {apps: {shop: {keep: forever}}}":    "backups of shop",
		"bucket: {bucket: b}":                         "access_key and secret_key",
		"watchers: [{token: t}]":                      "no url",
		"name: [":                                     "config.yml",
		"backups: {every: 1h, keeps: 7d}\nname: prod": "field keeps not found",
	} {
		useConfig(t, body)
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", body, err, want)
		}
	}
}

// A typo in the config stops nothing: the last good config keeps working,
// and the mistake is known.
func TestAMistakeKeepsTheLastGoodConfig(t *testing.T) {
	good := "name: prod\nbucket:\n  bucket: b\n  access_key: k\n  secret_key: s\nbackups:\n  keep: 30d\n"
	file := useConfig(t, good)
	b := newBackups(newApps(t.TempDir()))
	if !b.on() {
		t.Fatal("no bucket")
	}
	os.WriteFile(file, []byte(strings.Replace(good, "keep:", "kep:", 1)), 0o600)
	config, mistake := b.configNow()
	if mistake == nil || !strings.Contains(mistake.Error(), "kep") {
		t.Fatalf("the mistake: %v", mistake)
	}
	if _, keep := config.schedule("shop"); config.Bucket == nil || keep != 30*day || !b.on() {
		t.Fatalf("the last good config is gone: %+v", config)
	}
	if _, _, err := b.where("shop", protocol.Database{Path: "/data/app.db"}); err != nil {
		t.Fatalf("copies stop: %v", err)
	}
	os.WriteFile(file, []byte(good), 0o600)
	if _, mistake := b.configNow(); mistake != nil {
		t.Fatalf("after the fix: %v", mistake)
	}
	// A command that never read a good config says the mistake, not "no bucket".
	os.WriteFile(file, []byte("bucket: [\n"), 0o600)
	fresh := newBackups(newApps(t.TempDir()))
	if _, _, err := fresh.where("shop", protocol.Database{Path: "/data/app.db"}); err == nil || errors.Is(err, errNoBucket) {
		t.Fatalf("a config that never read: %v", err)
	}
}

// A schedule that copies too often or keeps too short is a mistake, not
// silently another schedule.
func TestScheduleLimits(t *testing.T) {
	for body, want := range map[string]string{
		"backups:\n  every: 1m\n":                        "every 1m is too often",
		"backups:\n  keep: 12h\n":                        "keep 12h is too short, the least is 1d",
		"backups:\n  apps:\n    shop:\n      keep: 5m\n": "backups of shop: keep 5m is too short",
	} {
		useConfig(t, body)
		if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
	useConfig(t, "backups:\n  every: 5m\n  keep: 1d\n")
	if _, err := LoadConfig(); err != nil {
		t.Errorf("the least of both: %v", err)
	}
}

// What a command changes in the config leaves your comments where they
// are, and never writes the hostname down as the server's name.
func TestSaveConfigKeepsCommentsAndNoHostname(t *testing.T) {
	file := useConfig(t, `# The config of our server.

# Where the backups go.
bucket:
  bucket: b # ours
  access_key: k
  secret_key: s
backups:
  # The shop changes all day.
  apps:
    shop:
      every: 15m # often
`)
	if err := ConnectWatcherHere(Watcher{URL: "https://watcher.example.com", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(file)
	for _, kept := range []string{"# The config of our server.", "# Where the backups go.", "bucket: b # ours", "# The shop changes all day.", "every: 15m # often", "url: https://watcher.example.com"} {
		if !strings.Contains(string(body), kept) {
			t.Errorf("%q is gone:\n%s", kept, body)
		}
	}
	if strings.Contains(string(body), "name:") {
		t.Errorf("the hostname was written down as the name:\n%s", body)
	}
	if config, err := LoadConfig(); err != nil || config.Name == "" || config.Backups.Apps["shop"].Every != "15m" {
		t.Fatalf("read back: %+v %v", config, err)
	}
}

// SaveConfig writes the file for root only, and it reads back the same.
func TestSaveConfig(t *testing.T) {
	file := useConfig(t, "")
	config := Config{Name: "prod", Alerts: []alert.Destination{{Kind: "webhook", URL: "https://example.com/hook"}}, Bucket: &Bucket{Bucket: "b", AccessKey: "k", SecretKey: "s"}}
	if err := SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config.yml: %v, %v", info, err)
	}
	again, err := LoadConfig()
	if err != nil || again.Bucket.SecretKey != "s" || again.Alerts[0].URL != "https://example.com/hook" {
		t.Fatalf("read back %+v, %v", again, err)
	}
	if err := SaveConfig(Config{Bucket: &Bucket{}}); err == nil {
		t.Fatal("saved a bucket without keys")
	}
}
