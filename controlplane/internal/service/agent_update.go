package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
