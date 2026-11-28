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
	"net/netip"
	neturl "net/url"
	"slices"
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
	// Knocks are the connections the server's firewall dropped; nil
	// while it counted none.
	Knocks *Knocks `json:"knocks,omitempty"`
	// Alerts are where the server's alerts go, so a watcher can alert
	// there when the server no longer can. They are secrets. A daemon
	// before this field, or a server without alerts, sends none.
	Alerts []Alert `json:"alerts,omitempty"`
}

// Alert is one destination for alerts. Kind is "telegram", "slack",
// "discord" or "webhook". Package alert sends to it.
type Alert struct {
	Kind   string `yaml:"kind" json:"kind"`
	URL    string `yaml:"url,omitempty" json:"url,omitempty"`         // slack, discord, webhook
	Token  string `yaml:"token,omitempty" json:"token,omitempty"`     // telegram bot token
	ChatID string `yaml:"chat_id,omitempty" json:"chat_id,omitempty"` // telegram
}

// Knocks are the knocks a server's firewall dropped: today and this
// year by the server's clock, all since the first, and today's by the
// name of the port, like "ssh" or "postgres".
type Knocks struct {
	Today       int64            `json:"today"`
	Year        int64            `json:"year"`
	Total       int64            `json:"total"`
	TodayByName map[string]int64 `json:"today_by_name"`
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

// checkURL wants https, or http to this machine or a tailnet only: the
// token and the alert destinations must not travel in the clear.
func checkURL(address string) error {
	parsed, err := neturl.Parse(address)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("the watcher url %q is not a valid URL", address)
	}
	if parsed.Scheme == "https" || parsed.Scheme == "http" && PlainHTTP(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf("the watcher url %q must start with https:// (http:// only to this machine or a tailnet address)", address)
}

// tailnets are the addresses Tailscale gives its devices.
var tailnets = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}

// PlainHTTP says whether http without TLS may go to host: this machine,
// or a host whose every address is in a tailnet, whose traffic WireGuard
// encrypts already. A name is looked up now.
func PlainHTTP(host string) bool {
	if host == "localhost" {
		return true
	}
	addresses := []string{host}
	if _, err := netip.ParseAddr(host); err != nil {
		addresses, err = net.LookupHost(host)
		if err != nil || len(addresses) == 0 {
			return false
		}
	}
	for _, address := range addresses {
		ip, err := netip.ParseAddr(address)
		if err != nil {
			return false
		}
		ip = ip.Unmap()
		if !ip.IsLoopback() && !slices.ContainsFunc(tailnets, func(tailnet netip.Prefix) bool { return tailnet.Contains(ip) }) {
			return false
		}
	}
	return true
}
