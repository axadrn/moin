// Package client talks to the control plane API for the CLI and the TUI.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/axadrn/ops/protocol"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/ops/internal/build"
)

// DefaultURL is the hosted control plane.
const DefaultURL = "https://ops.goilerplate.com"

// ErrNotLoggedIn means there is no token in the environment or config file.
var ErrNotLoggedIn = errors.New("not logged in: run `" + build.Name + " login`")

// Config is what `ops login` saves.
type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	// TokenFromEnv is true when the token came from OPS_TOKEN.
	TokenFromEnv bool `json:"-"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, build.Name, "config.json"), nil
}

// LoadConfig reads the config file. OPS_URL and OPS_TOKEN override it.
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
	if value := os.Getenv("OPS_URL"); value != "" {
		config.URL = value
	}
	if value := os.Getenv("OPS_TOKEN"); value != "" {
		config.Token, config.TokenFromEnv = value, true
	}
	if config.URL == "" {
		config.URL = DefaultURL
	}
	config.URL = strings.TrimRight(config.URL, "/")
	return config, nil
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
	return os.WriteFile(path, body, 0o600)
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

// Server is a server with an agent, as the API answers it.
type Server struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Online       bool       `json:"online"`
	Hostname     string     `json:"hostname"`
	OS           string     `json:"os"`
	Arch         string     `json:"arch"`
	AgentVersion string     `json:"agent_version"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	// Tunnel, SSHTailscale and Firewall say how the server is reached and
	// locked down; Security is what its agent set up.
	Tunnel       bool                         `json:"tunnel"`
	SSHTailscale bool                         `json:"ssh_tailscale"`
	Firewall     bool                         `json:"firewall"`
	Security     *protocol.ServerConfigResult `json:"security"`
	// Rules are what the owner allows ops to do on the server, from
	// /etc/ops/rules there. Nil without rules.
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
	if s.SSHTailscale {
		parts = append(parts, "ssh on tailnet")
	}
	return strings.Join(parts, ", ")
}

// NewServer is a server that waits for its agent.
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
}

// Client calls the control plane with a personal API token.
type Client struct {
	url          string
	token        string
	tokenFromEnv bool
	http         *http.Client
	// surface is "cli" or "tui". It goes in the user agent, so the activity
	// says where a change came from.
	surface string
}

// UseSurface names the program that sends the requests: "cli" or "tui".
func (c *Client) UseSurface(surface string) { c.surface = surface }

// New returns a client for the config, or ErrNotLoggedIn.
func New(config Config) (*Client, error) {
	if config.Token == "" {
		return nil, ErrNotLoggedIn
	}
	return &Client{url: config.URL, token: config.Token, tokenFromEnv: config.TokenFromEnv, http: &http.Client{}, surface: "cli"}, nil
}

// Account returns the user the token belongs to.
func (c *Client) Account(ctx context.Context) (Account, error) {
	var response struct {
		User Account `json:"user"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/account", nil, &response)
	return response.User, err
}

// Servers lists the servers of the account.
func (c *Client) Servers(ctx context.Context) ([]Server, error) {
	var response struct {
		Items []Server `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/servers", nil, &response)
	return response.Items, err
}

// AddServer creates a server and the install token for its agent.
func (c *Client) AddServer(ctx context.Context, name string) (NewServer, error) {
	var response NewServer
	err := c.do(ctx, http.MethodPost, "/api/v1/servers", map[string]string{"name": name}, &response)
	return response, err
}

// RenameServer gives a server a new name and returns it.
func (c *Client) RenameServer(ctx context.Context, idOrName, name string) (Server, error) {
	var response Server
	err := c.do(ctx, http.MethodPatch, "/api/v1/servers/"+url.PathEscape(idOrName), map[string]string{"name": name}, &response)
	return response, err
}

// UpdateAgent has the agent of a server download the control plane's ops
// binary, swap it in and restart. It returns the server once the new agent
// is back.
func (c *Client) UpdateAgent(ctx context.Context, idOrName string) (Server, error) {
	var response Server
	err := c.do(ctx, http.MethodPost, "/api/v1/servers/"+url.PathEscape(idOrName)+"/update", map[string]any{}, &response)
	return response, err
}

// Meta is what the control plane says about itself.
type Meta struct {
	Name string `json:"name"`
	// Version is the version of the control plane and of the ops binary
	// it serves; "dev" for a build without one.
	Version string `json:"version"`
}

// Meta returns the control plane's name and version.
func (c *Client) Meta(ctx context.Context) (Meta, error) {
	var response Meta
	err := c.do(ctx, http.MethodGet, "/api/v1/meta", nil, &response)
	return response, err
}

// Outdated reports whether the server's agent runs another version than
// the control plane. Development builds never count.
func (s Server) Outdated(version string) bool {
	return version != "" && version != "dev" && s.AgentVersion != "" && s.AgentVersion != version
}

// RemoveServer deletes a server by ID or name. Its agent is disconnected
// and stays installed: it answers the command that removes it on the server.
func (c *Client) RemoveServer(ctx context.Context, idOrName string) (string, error) {
	var removed struct {
		UninstallCommand string `json:"uninstall_command"`
	}
	err := c.do(ctx, http.MethodDelete, "/api/v1/servers/"+url.PathEscape(idOrName), nil, &removed)
	if err == nil && removed.UninstallCommand == "" {
		removed.UninstallCommand = protocol.UninstallCommand(build.Name)
	}
	return removed.UninstallCommand, err
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

// stream reads server-sent events and calls fn with each event name and its
// data, until ctx ends, fn returns an error or the stream breaks. fn returns
// errStreamDone to end it without an error.
func (c *Client) stream(ctx context.Context, path string, fn func(event string, data []byte) error) error {
	request, err := c.request(ctx, http.MethodGet, path, nil)
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
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("User-Agent", "ops-"+c.surface+"/"+build.Version)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, target any) error {
	request, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return c.responseError(response)
	}
	if target == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(target)
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

// rejected says what to do about a token the control plane does not take.
func (c *Client) rejected() string {
	if c.tokenFromEnv {
		return "The control plane rejected the token in OPS_TOKEN: it is wrong, expired or revoked. Set OPS_TOKEN to a valid token, or unset it to use the one from `" + build.Name + " login`"
	}
	return "The control plane rejected your token: it is wrong, expired or revoked. Run `" + build.Name + " login` for a new one"
}

// Status is "online", "offline" or "waiting" for a server whose agent never connected.
func (s Server) Status() string {
	switch {
	case s.Online:
		return "online"
	case s.LastSeenAt == nil:
		return "waiting"
	}
	return "offline"
}

// System is "linux/amd64", or a dash before the agent registered.
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
	age := now.Sub(*s.LastSeenAt)
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds ago", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(age.Hours()/24))
}
