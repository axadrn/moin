// Package watcher is `moin watcher`: the only part of moin that listens.
// Daemons post reports to it. It alerts when a server stops reporting and
// when a domain is down from outside, and shows one read-only page. It
// never sends anything to a server: nothing goes back but a status code.
package watcher

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"regexp"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
	"gopkg.in/yaml.v3"
)

// DefaultConfig is where the watcher reads its config.
const DefaultConfig = "/etc/" + build.Name + "/watcher.yml"

// Config is the watcher's one file:
//
//	password: …          # the page asks for it
//	servers:
//	  prod: <token>      # a server and the token it reports with
//	alerts:
//	  - kind: telegram
//	    token: …
//	    chat_id: …
type Config struct {
	Password string              `yaml:"password"`
	Servers  map[string]string   `yaml:"servers"`
	Alerts   []alert.Destination `yaml:"alerts"`
}

// serverName is what a server may be called: a name or user@host.
var serverName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._-]{0,126}$`)

// LoadConfig reads and checks the config at path.
func LoadConfig(path string) (Config, error) {
	var config Config
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("%s: %w", path, err)
	}
	if config.Password == "" {
		return config, fmt.Errorf("%s: set a password, the page asks for it", path)
	}
	seen := map[string]string{}
	for name, token := range config.Servers {
		if err := CheckName(name); err != nil {
			return config, fmt.Errorf("%s: %w", path, err)
		}
		if len(token) < 16 {
			return config, fmt.Errorf("%s: the token of %s is too short; make one with `%s watcher add %s`", path, name, build.Name, name)
		}
		if other, ok := seen[token]; ok {
			return config, fmt.Errorf("%s: %s and %s have the same token", path, other, name)
		}
		seen[token] = name
	}
	for _, d := range config.Alerts {
		if d.Kind != "telegram" && d.Kind != "slack" && d.Kind != "discord" && d.Kind != "webhook" {
			return config, fmt.Errorf("%s: unknown alert kind %q: use telegram, slack, discord or webhook", path, d.Kind)
		}
	}
	return config, nil
}

// CheckName says whether name can name a server.
func CheckName(name string) error {
	if !serverName.MatchString(name) {
		return fmt.Errorf("%q is not a server name: letters, digits, dots, dashes, underscores and @", name)
	}
	return nil
}

// NewToken is a new random token for a server: 128 bits in base32.
func NewToken() string {
	return rand.Text()
}
