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
//	password: …           # the page asks for it
//	servers:
//	  my-server: <token>  # a server and the token it reports with
//	alerts:               # optional: the watcher's own destinations
//	  - kind: telegram
//	    token: …
//	    chat_id: …
//
// Alerts go where each server's report says (`moin alerts add`). The
// alerts here get every alert too, and are all there is for a server
// that set none or runs an older moin.
type Config struct {
	Password string              `yaml:"password"`
	Servers  map[string]string   `yaml:"servers"`
	Alerts   []alert.Destination `yaml:"alerts"`
}

// minPassword is the shortest password the page takes.
const minPassword = 16

// serverName is what a server may be called: a name or user@host.
var serverName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._-]{0,126}$`)

// LoadConfig reads and checks the config at path. Only its owner may
// read it: it holds the tokens.
func LoadConfig(path string) (Config, error) {
	var config Config
	info, err := os.Stat(path)
	if err != nil {
		return config, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return config, fmt.Errorf("%s holds tokens, only its owner may read it: chmod 600 %s", path, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("%s: %w", path, err)
	}
	if err := config.check(); err != nil {
		return config, fmt.Errorf("%s: %w", path, err)
	}
	return config, nil
}

// check says what is missing or wrong in a config.
func (c Config) check() error {
	if len(c.Password) < minPassword {
		return fmt.Errorf("the password needs at least %d characters, the page asks for it", minPassword)
	}
	for _, d := range c.Alerts {
		if err := alert.Check(d); err != nil {
			return err
		}
	}
	seen := map[string]string{}
	for name, token := range c.Servers {
		if err := CheckName(name); err != nil {
			return err
		}
		if len(token) < 16 {
			return fmt.Errorf("the token of %s is too short; make one with `%s watcher token %s`", name, build.Name, name)
		}
		if other, ok := seen[token]; ok {
			return fmt.Errorf("%s and %s have the same token", other, name)
		}
		seen[token] = name
	}
	return nil
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
