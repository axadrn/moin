// Package agent is `ops agent`: the process on a server that dials out to the
// control plane and holds one WebSocket session to it.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/protocol"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// DefaultStateDir is where a root agent keeps its state. OPS_STATE_DIR
// overrides it, for example when an agent runs without root in development.
const DefaultStateDir = "/var/lib/ops"

const (
	stateFile     = "agent.json"
	minReconnect  = time.Second
	maxReconnect  = 30 * time.Second
	registerLimit = 10 * time.Second
)

// ErrNotRegistered means the agent has no state yet and needs `ops agent register`.
var ErrNotRegistered = errors.New("agent is not registered: run `" + build.Name + " agent register --url <control plane> --token <install token>`")

// State is what the agent keeps between restarts.
type State struct {
	URL        string `json:"url"`
	ServerID   string `json:"server_id"`
	AgentToken string `json:"agent_token"`
}

// StateDir returns OPS_STATE_DIR or DefaultStateDir.
func StateDir() string {
	dir := os.Getenv("OPS_STATE_DIR")
	if dir == "" {
		return DefaultStateDir
	}
	return dir
}

// LoadState reads the state from dir.
func LoadState(dir string) (State, error) {
	body, err := os.ReadFile(filepath.Join(dir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrNotRegistered
	}
	if err != nil {
		return State{}, err
	}
	var state State
	err = json.Unmarshal(body, &state)
	if err != nil {
		return State{}, fmt.Errorf("read %s: %w", stateFile, err)
	}
	if state.URL == "" || state.AgentToken == "" {
		return State{}, ErrNotRegistered
	}
	return state, nil
}

func saveState(dir string, state State) error {
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, stateFile), body, 0o600)
}

// Hello describes this agent and server.
func Hello() protocol.Hello {
	hostname, _ := os.Hostname()
	return protocol.Hello{
		ProtocolVersion: protocol.Version,
		AgentVersion:    build.Version,
		Hostname:        hostname,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
	}
}

// Register trades a one-time install token for an agent token and saves it
// in dir. Running it again with a new install token moves the agent to that
// server.
func Register(ctx context.Context, dir, baseURL, installToken string) (State, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	ctx, cancel := context.WithTimeout(ctx, registerLimit)
	defer cancel()

	body, err := json.Marshal(protocol.RegisterRequest{Hello: Hello()})
	if err != nil {
		return State{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+protocol.RegisterPath, bytes.NewReader(body))
	if err != nil {
		return State{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+installToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return State{}, fmt.Errorf("register: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return State{}, fmt.Errorf("register: %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	var registered protocol.RegisterResponse
	err = json.NewDecoder(response.Body).Decode(&registered)
	if err != nil {
		return State{}, fmt.Errorf("register: %w", err)
	}
	state := State{URL: baseURL, ServerID: registered.ServerID, AgentToken: registered.AgentToken}
	err = saveState(dir, state)
	if err != nil {
		return State{}, err
	}
	return state, nil
}

// Run holds the session until ctx ends and reconnects when it drops. A
// rejected agent token ends Run, because retrying cannot fix it.
func Run(ctx context.Context, state State) error {
	wait := minReconnect
	for {
		connected, err := session(ctx, state)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errRejected) {
			return err
		}
		if connected {
			wait = minReconnect
		}
		slog.Warn("session ended, reconnecting", "error", err, "in", wait)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		wait = min(wait*2, maxReconnect)
	}
}

var errRejected = errors.New("the control plane rejected the agent token: register this server again")

// session holds one WebSocket session. connected reports whether the dial
// succeeded, so Run can reset its backoff.
func session(ctx context.Context, state State) (connected bool, err error) {
	sessionURL, err := websocketURL(state.URL + protocol.SessionPath)
	if err != nil {
		return false, err
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+state.AgentToken)
	conn, response, err := websocket.Dial(ctx, sessionURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return false, errRejected
		}
		return false, err
	}
	defer conn.CloseNow()

	hello, err := protocol.NewMessage(protocol.TypeHello, Hello())
	if err != nil {
		return true, err
	}
	err = wsjson.Write(ctx, conn, hello)
	if err != nil {
		return true, err
	}
	slog.Info("connected", "url", state.URL, "server_id", state.ServerID)

	// Reading answers the control plane's pings. Later slices handle
	// requests here.
	for {
		var message protocol.Message
		err = wsjson.Read(ctx, conn, &message)
		if err != nil {
			return true, err
		}
		slog.Debug("message", "type", message.Type)
	}
}

func websocketURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	default:
		return "", fmt.Errorf("control plane URL must start with http:// or https://, got %q", raw)
	}
	return parsed.String(), nil
}
