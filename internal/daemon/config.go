package daemon

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"filippo.io/age"
	"gopkg.in/yaml.v3"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
)

// DefaultConfigFile is the config of a server: its name, who watches it,
// where alerts go, the bucket and how often each app is copied. Only root
// reads it, it holds the keys of the bucket. `moin daemon` reads it every
// minute, so a change counts without a restart. MOIN_CONFIG overrides it.
//
//	name: prod
//	watchers:
//	  - url: https://watcher.example.com
//	    token: …
//	alerts:
//	  - kind: telegram
//	    token: …
//	    chat_id: …
//	bucket:
//	  endpoint: https://s3.example.com
//	  region: auto
//	  bucket: backups
//	  path: moin
//	  access_key: …
//	  secret_key: …
//	backup_key:
//	  public: age1…
//	  confirmed: true
//	backups:
//	  every: 1h
//	  keep: 7d
//	  apps:
//	    shop:
//	      every: 15m
//	      keep: 30d
const DefaultConfigFile = "/etc/" + build.Name + "/config.yml"

// ConfigFile returns MOIN_CONFIG, else DefaultConfigFile.
func ConfigFile() string {
	if file := build.Getenv("CONFIG"); file != "" {
		return file
	}
	return DefaultConfigFile
}

// Config is the config file of a server.
type Config struct {
	// Name is the server's name, as the user called it: the SSH alias.
	Name     string              `yaml:"name,omitempty"`
	Watchers []Watcher           `yaml:"watchers,omitempty"`
	Alerts   []alert.Destination `yaml:"alerts,omitempty"`
	Bucket   *Bucket             `yaml:"bucket,omitempty"`
	// BackupKey is your key for the copies (backup_key.go).
	BackupKey *BackupKey `yaml:"backup_key,omitempty"`
	Backups   Backups    `yaml:"backups,omitempty"`
	// Mounts are folders of the server an app may mount, such as
	// /mnt/media. Without them an app mounts only its own folders.
	Mounts []string `yaml:"mounts,omitempty"`
	// ServerAccess allows compose settings that reach beyond the app, such
	// as privileged containers or a mount of the Docker socket.
	ServerAccess bool `yaml:"server_access,omitempty"`
}

// Watcher is one watcher the daemon reports to.
type Watcher struct {
	URL   string `yaml:"url" json:"url"`
	Token string `yaml:"token" json:"token"`
}

// Bucket is an S3-compatible bucket. The copies of a database live in
// <path>/<server>/<app>/<database>/copies/.
type Bucket struct {
	Endpoint  string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Region    string `yaml:"region,omitempty" json:"region,omitempty"`
	Bucket    string `yaml:"bucket" json:"bucket"`
	Path      string `yaml:"path,omitempty" json:"path,omitempty"`
	AccessKey string `yaml:"access_key" json:"access_key"`
	SecretKey string `yaml:"secret_key" json:"secret_key,omitempty"`
}

// BackupKey is the public half of your backup key, which the CLI made on
// your laptop: every copy is encrypted for it. Confirmed says you showed
// that you have the private half, by opening a copy with it.
type BackupKey struct {
	Public    string `yaml:"public" json:"public"`
	Confirmed bool   `yaml:"confirmed,omitempty" json:"confirmed"`
}

// Backups is how often the databases are copied and how long a copy is
// kept: one schedule for the server, and one for each app that has its own.
type Backups struct {
	Schedule `yaml:",inline"`
	Apps     map[string]Schedule `yaml:"apps,omitempty"`
}

// Schedule is how an app is backed up: a copy every Every, each kept
// Keep. Both are a number and m, h or d, like 15m, 1h or 7d. An empty one
// takes the server's, and that the default.
type Schedule struct {
	Every string `yaml:"every,omitempty"`
	Keep  string `yaml:"keep,omitempty"`
}

// The schedule of a server nobody set one for, and the least it takes.
const (
	defaultEvery = time.Hour
	defaultKeep  = 7 * day
	minEvery     = 5 * time.Minute
	minKeep      = day
)

// span reads 15m, 1h or 7d.
func span(word string) (time.Duration, error) {
	if len(word) < 2 {
		return 0, fmt.Errorf("%q is not a time span like 15m, 1h or 7d", word)
	}
	unit, ok := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': day}[word[len(word)-1]]
	number, err := strconv.Atoi(word[:len(word)-1])
	if !ok || err != nil || number < 1 || number > 10000 {
		return 0, fmt.Errorf("%q is not a time span like 15m, 1h or 7d", word)
	}
	return time.Duration(number) * unit, nil
}

// schedule is how often app is copied and how long a copy is kept. What
// is too short for it is a mistake in the config (check).
func (c Config) schedule(app string) (every, keep time.Duration) {
	every, keep = defaultEvery, defaultKeep
	for _, s := range []Schedule{c.Backups.Schedule, c.Backups.Apps[app]} {
		if d, err := span(s.Every); err == nil {
			every = d
		}
		if d, err := span(s.Keep); err == nil {
			keep = d
		}
	}
	return every, keep
}

// check says what is wrong in the config, so a typo never changes how long
// copies are kept without a word.
func (c Config) check() error {
	schedules := map[string]Schedule{"backups": c.Backups.Schedule}
	for app, schedule := range c.Backups.Apps {
		schedules["backups of "+app] = schedule
	}
	for where, schedule := range schedules {
		for _, word := range []string{schedule.Every, schedule.Keep} {
			if _, err := span(word); word != "" && err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
		}
		if every, err := span(schedule.Every); err == nil && every < minEvery {
			return fmt.Errorf("%s: every %s is too often, the least is 5m", where, schedule.Every)
		}
		if keep, err := span(schedule.Keep); err == nil && keep < minKeep {
			return fmt.Errorf("%s: keep %s is too short, the least is 1d", where, schedule.Keep)
		}
	}
	if b := c.Bucket; b != nil && (b.Bucket == "" || b.AccessKey == "" || b.SecretKey == "") {
		return errors.New("bucket: it needs bucket, access_key and secret_key")
	}
	if c.BackupKey != nil {
		if _, err := age.ParseX25519Recipient(c.BackupKey.Public); err != nil {
			return fmt.Errorf("backup_key: public is not the public half of a backup key, like age1…: %w", err)
		}
	}
	for _, watcher := range c.Watchers {
		if watcher.URL == "" {
			return errors.New("watchers: one has no url")
		}
	}
	return nil
}

// LoadConfig reads the config file. Without one, or without a name in
// it, the server has its hostname as name.
func LoadConfig() (Config, error) {
	config, err := readConfig()
	if err != nil {
		return Config{}, err
	}
	return named(config), nil
}

// named is config with the hostname as the name of a server without one.
// It is for reading only: what is saved never takes the hostname as name.
func named(config Config) Config {
	if config.Name == "" {
		config.Name, _ = os.Hostname()
	}
	return config
}

// readConfig reads the config file as it is written. A server without one
// has no bucket, watcher or alert.
func readConfig() (Config, error) {
	var config Config
	body, err := os.ReadFile(ConfigFile())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	// A key the config does not know is a typo, and an error.
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%s: %w", ConfigFile(), err)
	}
	if err := config.check(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", ConfigFile(), err)
	}
	return config, nil
}

// keepComments puts the comments of old on fresh, which holds the same
// config with new values: key by key in a map, item by item in a list.
func keepComments(old, fresh *yaml.Node) {
	if old.Kind != fresh.Kind {
		return
	}
	fresh.HeadComment, fresh.LineComment, fresh.FootComment = old.HeadComment, old.LineComment, old.FootComment
	switch fresh.Kind {
	case yaml.SequenceNode:
		for index := range min(len(old.Content), len(fresh.Content)) {
			keepComments(old.Content[index], fresh.Content[index])
		}
	case yaml.MappingNode:
		// A map is its keys and values in turn: key, value, key, value.
		for f := 0; f+1 < len(fresh.Content); f += 2 {
			for o := 0; o+1 < len(old.Content); o += 2 {
				if old.Content[o].Value == fresh.Content[f].Value {
					keepComments(old.Content[o], fresh.Content[f])
					keepComments(old.Content[o+1], fresh.Content[f+1])
				}
			}
		}
	}
}

// SaveConfig writes the config file whole, for root only. The comments
// you wrote into the file stay with their keys.
func SaveConfig(config Config) error {
	if err := config.check(); err != nil {
		return err
	}
	var fresh, old yaml.Node
	if err := fresh.Encode(config); err != nil {
		return err
	}
	if written, err := os.ReadFile(ConfigFile()); err == nil && yaml.Unmarshal(written, &old) == nil && len(old.Content) == 1 {
		keepComments(old.Content[0], &fresh)
		// The first comment of a file belongs to the file, not to a key.
		fresh.HeadComment = cmp.Or(old.HeadComment, fresh.HeadComment)
	}
	body, err := yaml.Marshal(&fresh)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ConfigFile()), 0o755); err != nil {
		return err
	}
	return writeWhole(ConfigFile(), body)
}
