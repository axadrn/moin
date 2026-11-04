package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/protocol"
)

// agentUpdateLimit is how long an update may take: the download, the swap
// and the agent coming back.
const agentUpdateLimit = 3 * time.Minute

// ErrNoAgentBinary means the control plane serves no binary for the
// server's OS and CPU.
var ErrNoAgentBinary = errors.New("no agent binary for this server")

// DownloadsDir is where the control plane keeps the binaries it serves
// under /download. The image builds them there.
const DownloadsDir = "downloads"

// AgentBinary returns the SHA-256 of the binary served for an OS and CPU.
func (s *ServerService) AgentBinary(goos, arch string) (string, error) {
	if goos == "" || arch == "" || strings.ContainsAny(goos+arch, `/\.`) {
		return "", ErrNoAgentBinary
	}
	file, err := os.Open(filepath.Join(s.downloads, build.Name+"-"+goos+"-"+arch))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoAgentBinary
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// UpdateAgent has the agent of a server swap its binary for the one the
// control plane serves, and returns the server once the new agent is back.
func (s *ServerService) UpdateAgent(ctx context.Context, actor Actor, idOrName string) (*model.Server, error) {
	server, err := s.Get(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, err
	}
	if !server.Online {
		return nil, ErrServerOffline
	}
	sum, err := s.AgentBinary(server.OS, server.Arch)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), agentUpdateLimit)
	defer cancel()
	s.mu.Lock()
	before := s.sessions[server.ID]
	s.mu.Unlock()
	err = s.Request(ctx, server.ID, protocol.TypeAgentUpdate, protocol.AgentUpdate{Version: build.Version, SHA256: sum}, nil)
	var agentErr *AgentError
	if errors.As(err, &agentErr) && strings.Contains(agentErr.Message, `does not know "`+string(protocol.TypeAgentUpdate)+`"`) {
		// Agents from before agent.update do not know it.
		return nil, &AgentError{Message: "This agent is too old to update itself. Update it once on the server, as root: " + protocol.UpdateCommand(s.appURL, build.Name, server.Arch)}
	}
	if err != nil {
		return nil, err
	}
	// An update by hand lets the automatic updates go on.
	if actor.Via != ViaSystem {
		s.mu.Lock()
		delete(s.autoHalted, actor.WorkspaceID)
		s.mu.Unlock()
	}
	// The agent restarts with the new binary: wait for its next session.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		current := s.sessions[server.ID]
		s.mu.Unlock()
		if current != nil && current != before {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("the agent took the new binary but did not come back: check it on the server with systemctl status %s-agent", build.Name)
		case <-ticker.C:
		}
	}
	updated, err := s.Get(actor.WorkspaceID, server.ID)
	if err != nil {
		return nil, err
	}
	// An agent that already ran this version changed nothing worth a row.
	if updated.AgentVersion != server.AgentVersion {
		s.record(actor, "server.agent_updated", "server", server.Name, map[string]string{"from": server.AgentVersion, "to": updated.AgentVersion})
	}
	return updated, nil
}

// AutoUpdateAgents reports whether the control plane updates the agents of
// the workspace to its own version by itself.
func (s *ServerService) AutoUpdateAgents(workspaceID string) (bool, error) {
	return s.servers.AutoUpdateAgents(workspaceID)
}

// SetAutoUpdateAgents turns updating the agents by themselves on or off.
// Turned on, it updates the agents that are behind right away.
func (s *ServerService) SetAutoUpdateAgents(actor Actor, on bool) error {
	before, err := s.servers.AutoUpdateAgents(actor.WorkspaceID)
	if err != nil {
		return err
	}
	if err := s.servers.SetAutoUpdateAgents(actor.WorkspaceID, on); err != nil {
		return err
	}
	if before != on {
		s.record(actor, "workspace.agent_updates", "workspace", "", map[string]string{"on": strconv.FormatBool(on)})
	}
	if on {
		s.mu.Lock()
		delete(s.autoHalted, actor.WorkspaceID)
		s.mu.Unlock()
		go s.autoUpdate(actor.WorkspaceID)
	}
	return nil
}

// UpdateAgentsTo has the control plane update every agent older than
// version by itself, whenever an agent connects. "" or a development build
// turns it off.
func (s *ServerService) UpdateAgentsTo(version string) {
	s.mu.Lock()
	s.autoVersion = version
	s.mu.Unlock()
}

// AgentBehind reports whether an agent should update to the control plane's
// release: it runs an older release, or a build without one, such as a
// commit like b4be272, which would otherwise never update.
func AgentBehind(release, agent string) bool {
	if parseVersion(release) == nil {
		return false
	}
	return parseVersion(agent) == nil || NewerVersion(release, agent)
}

// autoUpdate updates the agents of a workspace that are behind, one at a
// time. An agent that refuses, such as during a deploy, is tried again when
// it connects next. One that does not come back stops it for the workspace
// until someone updates an agent by hand, and the activity says so.
func (s *ServerService) autoUpdate(workspaceID string) {
	s.mu.Lock()
	version := s.autoVersion
	if parseVersion(version) == nil || s.autoRunning[workspaceID] || s.autoHalted[workspaceID] {
		s.mu.Unlock()
		return
	}
	s.autoRunning[workspaceID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.autoRunning, workspaceID)
		s.mu.Unlock()
	}()
	if on, err := s.servers.AutoUpdateAgents(workspaceID); err != nil || !on {
		return
	}
	tried := map[string]bool{}
	for {
		servers, err := s.List(workspaceID)
		if err != nil {
			slog.Error("list servers to update their agents", "error", err, "workspace_id", workspaceID)
			return
		}
		var next *model.Server
		for index, server := range servers {
			if server.Online && server.Rules == "" && !tried[server.ID] && AgentBehind(version, server.AgentVersion) {
				next = &servers[index]
				break
			}
		}
		if next == nil {
			return
		}
		tried[next.ID] = true
		_, err = s.UpdateAgent(context.Background(), System(workspaceID), next.ID)
		var agentErr *AgentError
		switch {
		case err == nil, errors.As(err, &agentErr), errors.Is(err, ErrServerOffline), errors.Is(err, ErrNoAgentBinary):
			// Updated, or refused for now: the next connect tries again.
		default:
			s.mu.Lock()
			s.autoHalted[workspaceID] = true
			s.mu.Unlock()
			s.record(System(workspaceID), "server.agent_update_failed", "server", next.Name, map[string]string{"from": next.AgentVersion, "to": version, "error": clipText(err.Error(), 300)})
			return
		}
	}
}
