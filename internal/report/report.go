// Package report is what a daemon tells its watchers: one POST of JSON
// with the state of its server. The daemon acts on nothing a watcher
// answers but the status code.
package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// Path is where a watcher takes reports, after its URL.
const Path = "/report"

// Report is the state of one server.
type Report struct {
	Server       string `json:"server"`        // the server's name, as the user called it at install
	Version      string `json:"version"`       // moin version on the server
	EverySeconds int    `json:"every_seconds"` // how often this daemon reports; silence = 3 × this, from 1 minute to 1 hour
	Apps         []App  `json:"apps"`
}

// App is one app on the server.
type App struct {
	Name      string     `json:"name"`
	Release   int        `json:"release"`
	Running   bool       `json:"running"`
	Domains   []string   `json:"domains"`
	Databases []Database `json:"databases"`
}

// Database is one database of an app and its backups.
type Database struct {
	Name        string    `json:"name"`                  // as the daemon names it today
	Kind        string    `json:"kind"`                  // "sqlite" or "postgres"
	LastCopy    time.Time `json:"last_copy,omitzero"`    // zero: never
	LastCheck   time.Time `json:"last_check,omitzero"`   // the last restore check that ran
	CheckOK     bool      `json:"check_ok"`              // whether that check restored the copy
	CheckedCopy time.Time `json:"checked_copy,omitzero"` // which copy the last good check proved
	Error       string    `json:"error,omitempty"`
}

var client = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Send posts r to one watcher, at its url and Path, with the token of
// this server. Any answer but a 2xx is an error.
func Send(ctx context.Context, url, token string, r Report) error {
	if err := checkURL(url); err != nil {
		return err
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/")+Path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", build.Name+"/"+build.Version)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 200))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		message := fmt.Sprintf("the watcher at %s answered HTTP %d", url, response.StatusCode)
		if text := strings.Join(strings.Fields(string(answer)), " "); text != "" {
			message += ": " + text
		}
		return errors.New(message)
	}
	return nil
}

// checkURL wants https, or http to this machine only: the token must not
// travel in the clear.
func checkURL(address string) error {
	parsed, err := neturl.Parse(address)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("the watcher url %q is not a valid URL", address)
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	loopback := host == "localhost" || ip != nil && ip.IsLoopback()
	if parsed.Scheme == "https" || parsed.Scheme == "http" && loopback {
		return nil
	}
	return fmt.Errorf("the watcher url %q must start with https:// (http:// only to this machine)", address)
}
