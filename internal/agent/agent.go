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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

// Hello describes this agent and server, with its rules.
func Hello() protocol.Hello {
	hostname, _ := os.Hostname()
	rules, _ := LoadRules(RulesFile())
	return protocol.Hello{
		Rules:           rules,
		ProtocolVersion: protocol.Version,
		AgentVersion:    build.Version,
		Hostname:        hostname,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Addresses:       publicAddresses(),
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
	// The firewall and tunnel of an earlier server entry must not outlive it.
	resetCtx, resetCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer resetCancel()
	resetNetwork(resetCtx, dir)
	return state, nil
}

// Run holds the session until ctx ends and reconnects when it drops. A
// rejected agent token ends Run, because retrying cannot fix it. Deploys
// keep running across reconnects.
func Run(ctx context.Context, dir string, state State) error {
	rulesFile := RulesFile()
	if _, err := LoadRules(rulesFile); err != nil {
		return err
	}
	agent := &runner{state: state, rulesFile: rulesFile, pending: map[string]protocol.DeployDone{}}
	agent.apps = newApps(dir, agent.send)
	agent.backups = newBackups(agent.apps, agent.send)
	agent.apps.backups = agent.backups
	agent.security = newSecurity(dir, agent.apps)
	agent.apps.proxyMode = agent.security.proxyMode
	agent.updater = newUpdater(state.URL)
	go agent.backups.drills(ctx)
	go agent.backups.pgSnapshots(ctx)
	go agent.backups.walgLoop(ctx)
	go agent.backups.health(ctx)
	go agent.apps.cleanups(ctx)
	go agent.backups.sweepUploads(ctx)
	go agent.metrics(ctx)
	defer agent.backups.configure(context.Background(), nil, nil)
	wait := minReconnect
	for {
		connected, err := agent.session(ctx)
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

// runner is one agent process. conn is the current session, nil between
// sessions.
type runner struct {
	state State
	// rulesFile is read for every request, so a change to it counts at once.
	rulesFile string
	apps      *apps
	backups   *backups
	security  *security
	updater   *updater
	// deploying counts the deploys that run now; an update waits for them.
	deploying atomic.Int32

	mu   sync.Mutex
	conn *websocket.Conn
	// pending holds the deploy.done messages no session took yet. They go
	// out on the next one, so a deploy never ends unseen.
	pending map[string]protocol.DeployDone
}

// send writes a message to the current session. Log lines without a session
// are dropped; a deploy.done waits for the next session.
func (r *runner) send(messageType protocol.MessageType, requestID string, data any) {
	message, err := protocol.NewMessage(messageType, data)
	if err != nil {
		slog.Error("encode message failed", "type", messageType, "error", err)
		return
	}
	message.RequestID = requestID
	r.mu.Lock()
	conn := r.conn
	if done, ok := data.(protocol.DeployDone); ok {
		r.pending[done.DeployID] = done
	}
	r.mu.Unlock()
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = wsjson.Write(ctx, conn, message)
	if err != nil {
		return
	}
	if done, ok := data.(protocol.DeployDone); ok {
		r.mu.Lock()
		delete(r.pending, done.DeployID)
		r.mu.Unlock()
	}
}

// session holds one WebSocket session. connected reports whether the dial
// succeeded, so Run can reset its backoff.
func (r *runner) session(ctx context.Context) (connected bool, err error) {
	sessionURL, err := websocketURL(r.state.URL + protocol.SessionPath)
	if err != nil {
		return false, err
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+r.state.AgentToken)
	conn, response, err := websocket.Dial(ctx, sessionURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return false, errRejected
		}
		return false, err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	hello, err := protocol.NewMessage(protocol.TypeHello, Hello())
	if err != nil {
		return true, err
	}
	err = wsjson.Write(ctx, conn, hello)
	if err != nil {
		return true, err
	}
	slog.Info("connected", "url", r.state.URL, "server_id", r.state.ServerID)

	r.mu.Lock()
	r.conn = conn
	pending := make([]protocol.DeployDone, 0, len(r.pending))
	for _, done := range r.pending {
		pending = append(pending, done)
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.conn == conn {
			r.conn = nil
		}
		r.mu.Unlock()
	}()
	for _, done := range pending {
		r.send(protocol.TypeDeployDone, "", done)
	}

	// Reading also answers the control plane's pings.
	for {
		var message protocol.Message
		err = wsjson.Read(ctx, conn, &message)
		if err != nil {
			return true, err
		}
		go r.handle(ctx, message)
	}
}

// handle runs one request from the control plane.
func (r *runner) handle(ctx context.Context, message protocol.Message) {
	fail := func(err error) {
		r.send(protocol.TypeError, message.RequestID, protocol.Error{Message: err.Error()})
	}
	// A rules file with a mistake refuses everything until it is fixed.
	rules, err := LoadRules(r.rulesFile)
	if err != nil {
		slog.Error("rules", "error", err)
		if message.Type == protocol.TypeDeployStart {
			var start protocol.DeployStart
			_ = message.Decode(&start)
			r.send(protocol.TypeDeployDone, "", protocol.DeployDone{DeployID: start.DeployID, Error: err.Error()})
		} else if message.RequestID != "" {
			fail(err)
		}
		return
	}
	switch message.Type {
	case protocol.TypeDeployStart:
		var start protocol.DeployStart
		if err := message.Decode(&start); err != nil {
			slog.Error("bad deploy.start", "error", err)
			return
		}
		if err := checkDeploy(rules, r.rulesFile, start); err != nil {
			slog.Warn("deploy refused", "app", start.App, "number", start.Number, "error", err)
			r.send(protocol.TypeDeployDone, "", protocol.DeployDone{DeployID: start.DeployID, Error: err.Error()})
			return
		}
		slog.Info("deploy", "app", start.App, "number", start.Number)
		r.deploying.Add(1)
		defer r.deploying.Add(-1)
		r.apps.deploy(ctx, start)
	case protocol.TypeLogs:
		var request protocol.LogsRequest
		if err := message.Decode(&request); err != nil {
			fail(err)
			return
		}
		if rules != nil && rules.LogsOff {
			fail(refused(r.rulesFile, "logs: off"))
			return
		}
		result, err := r.apps.logs(ctx, request)
		if err != nil {
			fail(err)
			return
		}
		r.send(protocol.TypeLogsResult, message.RequestID, result)
	case protocol.TypeAppRemove:
		var request protocol.AppRemove
		if err := message.Decode(&request); err != nil {
			fail(err)
			return
		}
		if err := r.apps.remove(ctx, request.App); err != nil {
			fail(err)
			return
		}
		r.send(protocol.TypeOK, message.RequestID, nil)
	case protocol.TypeAppStop:
		var request protocol.AppStop
		if err := message.Decode(&request); err != nil {
			fail(err)
			return
		}
		if err := r.apps.stop(ctx, request.App); err != nil {
			fail(err)
			return
		}
		r.send(protocol.TypeOK, message.RequestID, nil)
	case protocol.TypeDBTables:
		var request protocol.DBRequest
		if err := message.Decode(&request); err != nil {
			fail(err)
			return
		}
		if rules != nil && rules.DataOff {
			fail(refused(r.rulesFile, "data: off"))
			return
		}
		result, err := r.apps.tables(ctx, request)
		if err != nil {
			fail(err)
			return
		}
		r.send(protocol.TypeDBTablesResult, message.RequestID, result)
	case protocol.TypeDBQuery:
		var request protocol.DBQuery
		if err := message.Decode(&request); err != nil {
			fail(err)
			return
		}
		if rules != nil && rules.DataOff {
			fail(refused(r.rulesFile, "data: off"))
			return
		}
		result, err := r.apps.query(ctx, request)
		if err != nil {
			fail(err)
			return
		}
		r.send(protocol.TypeDBQueryResult, message.RequestID, result)
	case protocol.TypeServerConfig:
		var config protocol.ServerConfig
		if err := message.Decode(&config); err != nil {
			fail(err)
			return
		}
		if rules != nil && rules.NetworkLocked {
			fail(refused(r.rulesFile, "network: locked"))
			return
		}
		r.send(protocol.TypeServerConfigResult, message.RequestID, r.security.apply(ctx, config))
	case protocol.TypeBackupConfig:
		var config protocol.BackupConfig
		if err := message.Decode(&config); err != nil {
			fail(err)
			return
		}
		if err := checkStorage(rules, r.rulesFile, config.Storage); err != nil {
			fail(err)
			return
		}
		for _, storage := range config.AppStorage {
			if err := checkStorage(rules, r.rulesFile, storage); err != nil {
				fail(err)
				return
			}
		}
		kept := r.apps.keepIDs(config.Apps)
		if err := r.backups.configure(ctx, config.Storage, config.AppStorage); err != nil {
			fail(err)
			return
		}
		if kept {
			r.backups.reconcile()
		}
		r.send(protocol.TypeOK, message.RequestID, nil)
	case protocol.TypeBackupSnapshot, protocol.TypeBackupList:
		var request protocol.BackupRequest
		if err := message.Decode(&request); err != nil {
			fail(err)
			return
		}
		// Older buckets the rules do not allow stay unread.
		request.Older = slices.DeleteFunc(request.Older, func(storage protocol.BackupStorage) bool {
			return checkStorage(rules, r.rulesFile, &storage) != nil
		})
		list := r.backups.list
		if message.Type == protocol.TypeBackupSnapshot {
			list = r.backups.snapshot
		}
		result, err := list(ctx, request)
		if err != nil {
			fail(err)
			return
		}
		r.send(protocol.TypeBackupsResult, message.RequestID, result)
	case protocol.TypeBackupRestore:
		var request protocol.BackupRestore
		if err := message.Decode(&request); err != nil {
			fail(err)
			return
		}
		if err := checkStorage(rules, r.rulesFile, request.Storage); err != nil {
			fail(err)
			return
		}
		if err := r.backups.restore(ctx, request); err != nil {
			fail(err)
			return
		}
		r.send(protocol.TypeOK, message.RequestID, nil)
	case protocol.TypeExecStart:
		var start protocol.ExecStart
		if err := message.Decode(&start); err != nil {
			fail(err)
			return
		}
		if rules != nil && rules.ExecOff {
			fail(refused(r.rulesFile, "exec: off"))
			return
		}
		slog.Info("exec", "app", start.App, "service", start.Service)
		done, err := r.apps.runExec(ctx, start, func(data string) {
			r.send(protocol.TypeExecOutput, message.RequestID, protocol.ExecOutput{Data: data})
		})
		if err != nil {
			fail(err)
			return
		}
		r.send(protocol.TypeExecDone, message.RequestID, done)
	case protocol.TypeAgentUpdate:
		var update protocol.AgentUpdate
		if err := message.Decode(&update); err != nil {
			fail(err)
			return
		}
		// A new binary could allow what the rules stop, so with rules only
		// the owner updates it, on the server.
		if rules != nil {
			fail(fmt.Errorf("this server has rules in %s, so only its owner updates the agent, on the server: %s", r.rulesFile, protocol.UpdateCommand(r.state.URL, build.Name, runtime.GOARCH)))
			return
		}
		if r.deploying.Load() > 0 {
			fail(errors.New("a deploy is running: update the agent when it is done"))
			return
		}
		slog.Info("update", "from", build.Version, "to", update.Version)
		if err := r.updater.install(ctx, update); err != nil {
			slog.Error("update failed", "error", err)
			fail(err)
			return
		}
		r.send(protocol.TypeOK, message.RequestID, nil)
		r.updater.restart()
	default:
		if message.RequestID != "" {
			fail(fmt.Errorf("this agent does not know %q: update it", message.Type))
		}
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
