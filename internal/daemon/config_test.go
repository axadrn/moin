package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/internal/alert"
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
