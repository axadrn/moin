// Package client talks to the control plane API for the CLI and the TUI.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
		config.Token = value
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
	CreatedAt    time.Time  `json:"created_at"`
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
	url   string
	token string
	http  *http.Client
}

// New returns a client for the config, or ErrNotLoggedIn.
func New(config Config) (*Client, error) {
	if config.Token == "" {
		return nil, ErrNotLoggedIn
	}
	return &Client{url: config.URL, token: config.Token, http: &http.Client{}}, nil
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

// RemoveServer deletes a server by ID or name. Its agent is disconnected.
func (c *Client) RemoveServer(ctx context.Context, idOrName string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/servers/"+url.PathEscape(idOrName), nil, nil)
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
		return responseError(response)
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
		return responseError(response)
	}
	if target == nil {
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

// responseError turns an API error body into an error people can read.
func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	var problem struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &problem) == nil && problem.Error.Message != "" {
		message := problem.Error.Message
		for field, detail := range problem.Error.Fields {
			message += fmt.Sprintf(" %s: %s", field, detail)
		}
		if response.StatusCode == http.StatusUnauthorized {
			message = fmt.Sprintf("%s. Run `%s login` again", message, build.Name)
		}
		return &Error{Status: response.StatusCode, Code: problem.Error.Code, Message: message}
	}
	if response.StatusCode == http.StatusUnauthorized {
		return &Error{Status: response.StatusCode, Code: "unauthenticated", Message: fmt.Sprintf("token rejected. Run `%s login` again", build.Name)}
	}
	return &Error{Status: response.StatusCode, Message: fmt.Sprintf("%s %s: %s", response.Request.Method, response.Request.URL.Path, response.Status)}
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
