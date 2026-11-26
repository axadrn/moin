// Package client talks to the hub API for the CLI and the TUI.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/axadrn/moin/protocol"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/human"
)

// DefaultURL is the hosted hub.
const DefaultURL = "https://moin.run"

// ErrNotLoggedIn means there is no token in the environment or config file.
var ErrNotLoggedIn = errors.New("not logged in: run `" + build.Name + " login`")

// Config is what `moin login` saves. A login in the browser keeps the
// access token of its connection in Token, the refresh token and when the
// access token ends; the client refreshes them by itself. A CI token has
// only Token.
type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	// RefreshToken gets the next pair once ExpiresAt comes.
	RefreshToken string     `json:"refresh_token,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	// Connection is the ID of the connection the login made.
	Connection string `json:"connection,omitempty"`
	// TokenFromEnv is true when the token came from MOIN_TOKEN.
	TokenFromEnv bool `json:"-"`
	urlFromEnv   bool
}

// Login names the login for the TUI's cache: the connection, or a CI token
// itself.
func (c Config) Login() string {
	if c.Connection != "" {
		return "connection:" + c.Connection
	}
	return c.Token
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, build.Name, "config.json"), nil
}

// TokenVariable overrides the saved token, for CI.
var TokenVariable = strings.ToUpper(build.Name) + "_TOKEN"

// LoadConfig reads the config file. MOIN_URL and MOIN_TOKEN override it.
func LoadConfig() (Config, error) {
	var config Config
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	body, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	if err == nil {
		err = json.Unmarshal(body, &config)
		if err != nil {
			return Config{}, fmt.Errorf("read %s: %w", path, err)
		}
	}
	if value := build.Getenv("URL"); value != "" {
		config.URL = value
		config.urlFromEnv = true
	}
	if value := build.Getenv("TOKEN"); value != "" {
		config = Config{URL: config.URL, Token: value, TokenFromEnv: true, urlFromEnv: config.urlFromEnv}
	}
	if config.URL == "" {
		config.URL = DefaultURL
	}
	config.URL = strings.TrimRight(config.URL, "/")
	return config, nil
}

// LoadSaved reads the saved config alone, without the environment.
func LoadSaved() (Config, error) {
	var config Config
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, err
	}
	return config, json.Unmarshal(body, &config)
}

// SaveConfig writes the config file, readable only by the user.
func SaveConfig(config Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	// A temporary file in the same folder, 0600 as CreateTemp makes it,
	// renamed over the config: a process that dies while writing never
	// leaves half a login behind.
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// RemoveConfig deletes the config file.
func RemoveConfig() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Server is a server with a daemon, as the API answers it.
type Server struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Online        bool       `json:"online"`
	Hostname      string     `json:"hostname"`
	OS            string     `json:"os"`
	Arch          string     `json:"arch"`
	DaemonVersion string     `json:"daemon_version"`
	LastSeenAt    *time.Time `json:"last_seen_at"`
	// Tunnel, SSH and Firewall say how the server should be reached and
	// locked down; SSH is tailscale, everyone or off. Security is what its
	// daemon last saw on it.
	Tunnel   bool                         `json:"tunnel"`
	SSH      string                       `json:"ssh"`
	Firewall bool                         `json:"firewall"`
	Security *protocol.ServerConfigResult `json:"security"`
	// Rules are what the owner allows moin to do on the server, from
	// /etc/moin/rules there. Nil without rules.
	Rules     *protocol.Rules `json:"rules"`
	CreatedAt time.Time       `json:"created_at"`
}

// RulesText names the server's rules in a few words, or "-" without.
func (s Server) RulesText() string {
	var names []string
	for _, line := range s.Rules.Lines() {
		key, value, _ := strings.Cut(line, ": ")
		switch key {
		case "deploy-from", "backups-to":
			names = append(names, key)
		default:
			names = append(names, key+" "+value)
		}
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

// Access says in a few words how the server is reached.
func (s Server) Access() string {
	parts := []string{"ports 80, 443"}
	if s.Tunnel {
		parts = []string{"tunnel"}
	}
	if s.Firewall {
		parts = append(parts, "firewall")
	}
	if s.SSH != "" && s.SSH != protocol.SSHEveryone {
		parts = append(parts, "ssh "+s.SSH)
	}
	return strings.Join(parts, ", ")
}

// SecurityLine is one line of how a server is reached: what is on the
// server (Is) and what is set (Set), the lines of its Security page.
type SecurityLine struct {
	Name, Is, Set string
}

// SecurityLines are web, SSH and the firewall, the lines of the Security
// page in the same words, once the daemon reported; nil before.
func (s Server) SecurityLines() []SecurityLine {
	if s.Security == nil {
		return nil
	}
	web := func(tunnel bool) string {
		if tunnel {
			return "cloudflare tunnel"
		}
		return "open"
	}
	ssh := func(ssh string) string {
		switch ssh {
		case protocol.SSHTailscale:
			return "only through tailscale"
		case protocol.SSHOff:
			return "off"
		}
		return "everyone"
	}
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	return []SecurityLine{
		{"web", web(s.Security.Proxy == "tunnel"), web(s.Tunnel)},
		{"ssh", ssh(s.Security.SSH), ssh(s.SSH)},
		{"firewall", onOff(s.Security.Firewall), onOff(s.Firewall)},
	}
}

// NewServer is a server that waits for its daemon.
type NewServer struct {
	Server         Server    `json:"server"`
	InstallToken   string    `json:"install_token"`
	InstallCommand string    `json:"install_command"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// Account is the signed-in user.
type Account struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	// EmailAlerts mails the user when a site or a server goes down or
	// comes back, or a deploy fails.
	EmailAlerts bool `json:"email_alerts"`
}

// Client calls the hub as a connection or with a CI token.
type Client struct {
	url          string
	tokenFromEnv bool
	// login is the token and, for a connection, how to refresh it. mu
	// guards it: the TUI sends requests at once.
	mu    sync.Mutex
	login Config
	// config is the config the client came from; moved keeps a move to a
	// new address to once.
	config Config
	moved  sync.Once
	http   *http.Client
	// surface is "cli" or "tui". It goes in the user agent, so the activity
	// says where a change came from.
	surface string
}

// UseSurface names the program that sends the requests: "cli" or "tui".
func (c *Client) UseSurface(surface string) { c.surface = surface }

// URL is the hub this client talks to, without a slash at the end.
func (c *Client) URL() string { return c.url }

// New returns a client for the config, or ErrNotLoggedIn.
func New(config Config) (*Client, error) {
	if config.Token == "" {
		return nil, ErrNotLoggedIn
	}
	return &Client{url: config.URL, login: config, tokenFromEnv: config.TokenFromEnv, config: config, http: &http.Client{}, surface: "cli"}, nil
}

// Account returns the user the token belongs to.
func (c *Client) Account(ctx context.Context) (Account, error) {
	var response struct {
		User Account `json:"user"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/account", nil, &response)
	return response.User, err
}

// SetEmailAlerts turns the email alerts of the signed-in user on or off.
func (c *Client) SetEmailAlerts(ctx context.Context, on bool) error {
	return c.do(ctx, http.MethodPut, "/api/v1/account/email-alerts", struct {
		On bool `json:"on"`
	}{on}, nil)
}

// Servers lists the servers of the account.
func (c *Client) Servers(ctx context.Context) ([]Server, error) {
	var response struct {
		Items []Server `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/servers", nil, &response)
	return response.Items, err
}

// AddServer creates a server and the install token for its daemon.
func (c *Client) AddServer(ctx context.Context, name string) (NewServer, error) {
	var response NewServer
	err := c.do(ctx, http.MethodPost, "/api/v1/servers", map[string]string{"name": name}, &response)
	return response, err
}

// NewInstallCommand makes a new one-time install command for a server; the
// old one stops working.
func (c *Client) NewInstallCommand(ctx context.Context, idOrName string) (NewServer, error) {
	var response NewServer
	err := c.do(ctx, http.MethodPost, "/api/v1/servers/"+url.PathEscape(idOrName)+"/install", nil, &response)
	return response, err
}

// RenameServer gives a server a new name and returns it.
func (c *Client) RenameServer(ctx context.Context, idOrName, name string) (Server, error) {
	var response Server
	err := c.do(ctx, http.MethodPatch, "/api/v1/servers/"+url.PathEscape(idOrName), map[string]string{"name": name}, &response)
	return response, err
}

// UpdateDaemon has the daemon of a server download the hub's moin
// binary, swap it in and restart. It returns the server once the new daemon
// is back.
func (c *Client) UpdateDaemon(ctx context.Context, idOrName string) (Server, error) {
	var response Server
	err := c.do(ctx, http.MethodPost, "/api/v1/servers/"+url.PathEscape(idOrName)+"/update", map[string]any{}, &response)
	return response, err
}

// Meta is what the hub says about itself.
type Meta struct {
	Name string `json:"name"`
	// Version is the version of the hub and of the moin binary
	// it serves; "dev" for a build without one.
	Version string `json:"version"`
}

// Meta returns the hub's name and version.
func (c *Client) Meta(ctx context.Context) (Meta, error) {
	var response Meta
	err := c.do(ctx, http.MethodGet, "/api/v1/meta", nil, &response)
	return response, err
}

// Outdated reports whether the server's daemon runs another version than
// the hub. Development builds never count.
func (s Server) Outdated(version string) bool {
	return version != "" && version != "dev" && s.DaemonVersion != "" && s.DaemonVersion != version
}

// RemovedServer is a server just removed: the command that removes its
// daemon on the machine, empty when no daemon ever connected there, and the
// apps that went with it because it was offline.
type RemovedServer struct {
	UninstallCommand string
	Apps             []string
}

// RemoveServer deletes a server by ID or name. Its daemon is disconnected
// and stays installed. An offline server goes with its apps. confirm is the
// server's name, typed out; the hub checks it.
func (c *Client) RemoveServer(ctx context.Context, idOrName, confirm string) (RemovedServer, error) {
	var removed struct {
		UninstallCommand *string  `json:"uninstall_command"`
		Apps             []string `json:"apps"`
	}
	err := c.do(ctx, http.MethodDelete, "/api/v1/servers/"+url.PathEscape(idOrName)+"?confirm="+url.QueryEscape(confirm), nil, &removed)
	if err != nil {
		return RemovedServer{}, err
	}
	// A hub from before uninstall_command answers none.
	if removed.UninstallCommand == nil {
		return RemovedServer{UninstallCommand: protocol.UninstallCommand(build.Name), Apps: removed.Apps}, nil
	}
	return RemovedServer{UninstallCommand: *removed.UninstallCommand, Apps: removed.Apps}, nil
}

// WatchServers calls fn with the full server list on connect and on every
// change, until ctx ends or the stream breaks.
func (c *Client) WatchServers(ctx context.Context, fn func([]Server)) error {
	return c.stream(ctx, "/api/v1/servers/events", func(_ string, data []byte) error {
		var event struct {
			Items []Server `json:"items"`
		}
		err := json.Unmarshal(data, &event)
		if err == nil {
			fn(event.Items)
		}
		return err
	})
}

// errStreamDone ends a stream on purpose.
var errStreamDone = errors.New("stream done")

// errStreamReconnect is the hub's event reconnect: the stream ended with
// its access token, and the client comes back with a fresh one.
var errStreamReconnect = errors.New("stream ended with its access token")

// stream reads server-sent events and calls fn with each event name and its
// data, until ctx ends, fn returns an error or the stream breaks. fn returns
// errStreamDone to end it without an error. When the hub ends the stream
// with its access token, stream opens it again with a fresh one; the hub
// sends the full list again.
func (c *Client) stream(ctx context.Context, path string, fn func(event string, data []byte) error) error {
	for {
		err := c.streamRequest(ctx, http.MethodGet, path, nil, fn)
		if !errors.Is(err, errStreamReconnect) || ctx.Err() != nil {
			return err
		}
	}
}

// streamRequest is stream for any method, with a JSON body.
func (c *Client) streamRequest(ctx context.Context, method, path string, body any, fn func(event string, data []byte) error) error {
	request, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return c.responseError(response)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	event := ""
	for scanner.Scan() {
		line := scanner.Text()
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			event = name
			if event == "reconnect" {
				return errStreamReconnect
			}
			continue
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		err = fn(event, []byte(data))
		if errors.Is(err, errStreamDone) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}
	return io.ErrUnexpectedEOF
}

func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.url+path, reader)
	if err != nil {
		return nil, err
	}
	if token, err := c.token(ctx); err != nil {
		return nil, err
	} else if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("User-Agent", build.Name+"-"+c.surface+"/"+build.Version)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, target any) error {
	return c.send(ctx, method, path, body, target, true)
}

// send is do; retry refreshes the login once when the hub no longer takes
// the access token.
func (c *Client) send(ctx context.Context, method, path string, body, target any, retry bool) error {
	request, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// An access token the hub no longer takes, before its time: refresh
	// once and send again.
	if response.StatusCode == http.StatusUnauthorized && retry && c.refreshable() {
		err := c.refresh(ctx, request.Header.Get("Authorization"))
		if err == nil {
			response.Body.Close()
			return c.send(ctx, method, path, body, target, false)
		}
		if errors.Is(err, ErrLoginEnded) {
			return err
		}
	}
	c.follow(response)
	if response.StatusCode >= 300 {
		return c.responseError(response)
	}
	if target == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(target)
}

// MovedHeader names the hub's address in an answer to a request
// that came on an old one.
const MovedHeader = "X-Hub-URL"

// follow moves the saved config to the hub's new address when an
// answer names one: the next command dials it. A URL from the environment
// is the user's and stays.
func (c *Client) follow(response *http.Response) {
	to := strings.TrimRight(response.Header.Get(MovedHeader), "/")
	if to == "" || to == c.url || c.config.urlFromEnv || !strings.HasPrefix(to, "https://") {
		return
	}
	c.moved.Do(func() {
		saved, err := LoadSaved()
		if err != nil || saved.Token == "" {
			return
		}
		saved.URL = to
		_ = SaveConfig(saved)
	})
}

// Error is an error answer of the API.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// IsNotFound reports whether err is a not_found answer.
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Code == "not_found"
}

// IsClientOutdated reports whether the hub refused this moin as too old.
// The message says how to update.
func IsClientOutdated(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Code == "client_outdated"
}

// IsReadOnlyQuery reports whether a read query was refused because it
// changes data.
func IsReadOnlyQuery(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Code == "read_only_query"
}

// responseError turns an API error body into an error people can read.
func (c *Client) responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if response.StatusCode == http.StatusUnauthorized {
		return &Error{Status: response.StatusCode, Code: "unauthenticated", Message: c.rejected()}
	}
	var problem struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &problem) == nil && problem.Error.Message != "" {
		message := problem.Error.Message
		if len(problem.Error.Fields) > 0 {
			// The message points at the highlighted fields of the web form;
			// the fields themselves say what is wrong.
			parts := []string{}
			for _, field := range slices.Sorted(maps.Keys(problem.Error.Fields)) {
				parts = append(parts, strings.ReplaceAll(field, "_", " ")+": "+problem.Error.Fields[field])
			}
			message = strings.Join(parts, " ")
		}
		return &Error{Status: response.StatusCode, Code: problem.Error.Code, Message: message}
	}
	return &Error{Status: response.StatusCode, Message: fmt.Sprintf("%s %s: %s", response.Request.Method, response.Request.URL.Path, response.Status)}
}

// rejected says what to do about a token the hub does not take.
func (c *Client) rejected() string {
	if c.tokenFromEnv {
		return "The hub rejected the token in " + TokenVariable + ": it is wrong, expired or revoked. Set " + TokenVariable + " to a valid token, or unset it to use the one from `" + build.Name + " login`"
	}
	return "The hub rejected your token: it is wrong, expired or revoked. Run `" + build.Name + " login` for a new one"
}

// Status is "online", "offline" or "waiting" for a server whose daemon never connected.
func (s Server) Status() string {
	switch {
	case s.Online:
		return "online"
	case s.LastSeenAt == nil:
		return "waiting"
	}
	return "offline"
}

// System is "linux/amd64", or a dash before the daemon registered.
func (s Server) System() string {
	if s.OS == "" {
		return "-"
	}
	return s.OS + "/" + s.Arch
}

// LastSeen is "now" for online servers and a rough age for the others.
func (s Server) LastSeen(now time.Time) string {
	if s.Online {
		return "now"
	}
	if s.LastSeenAt == nil {
		return "-"
	}
	return human.Ago(*s.LastSeenAt, now)
}
