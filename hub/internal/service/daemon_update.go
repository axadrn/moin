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

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// daemonUpdateLimit is how long an update may take: the download, the swap
// and the daemon coming back.
const daemonUpdateLimit = 3 * time.Minute

// ErrNoDaemonBinary means the hub serves no binary for the
// server's OS and CPU.
var ErrNoDaemonBinary = errors.New("no daemon binary for this server")

// DownloadsDir is where the hub keeps the binaries it serves
// under /download. The image builds them there.
const DownloadsDir = "downloads"

// DaemonBinary returns the SHA-256 of the binary served for an OS and CPU.
func (s *ServerService) DaemonBinary(goos, arch string) (string, error) {
	if goos == "" || arch == "" || strings.ContainsAny(goos+arch, `/\.`) {
		return "", ErrNoDaemonBinary
	}
	file, err := os.Open(filepath.Join(s.downloads, build.Name+"-"+goos+"-"+arch))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoDaemonBinary
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

// UpdateDaemon has the daemon of a server swap its binary for the one the
// hub serves, with the steps its version needs (daemon_steps.go), and
// returns the server once the new daemon is back.
func (s *ServerService) UpdateDaemon(ctx context.Context, actor Actor, idOrName string) (*model.Server, error) {
	server, err := s.Get(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, err
	}
	if !server.Online {
		return nil, ErrServerOffline
	}
	if _, err := s.DaemonBinary(server.OS, server.Arch); err != nil {
		return nil, err
	}
	// An update left halfway is finished first.
	s.finishDaemonUpdate(server.WorkspaceID, server.ID)
	return s.runDaemonUpdate(ctx, actor, server)
}

// swapDaemon is the update itself: the daemon swaps its binary and comes
// back. The step updateDaemon runs it.
func (s *ServerService) swapDaemon(ctx context.Context, actor Actor, server *model.Server) (*model.Server, error) {
	sum, err := s.DaemonBinary(server.OS, server.Arch)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), daemonUpdateLimit)
	defer cancel()
	s.mu.Lock()
	before := s.sessions[server.ID]
	s.mu.Unlock()
	err = s.Request(ctx, server.ID, protocol.TypeDaemonUpdate, protocol.DaemonUpdate{Version: build.Version, SHA256: sum}, nil)
	var daemonErr *DaemonError
	if errors.As(err, &daemonErr) && strings.Contains(daemonErr.Message, `does not know "`+string(protocol.WireType(protocol.TypeDaemonUpdate, 1))+`"`) {
		// Agents from before agent.update, long before the daemon, do not know it.
		return nil, &DaemonError{Message: "This daemon is too old to update itself. Update it once on the server, as root: " + protocol.LegacyUpdateCommand(s.appURL, build.Name, server.Arch)}
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
	// The daemon restarts with the new binary: wait for its next session.
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
			return nil, fmt.Errorf("the daemon took the new binary but did not come back: check it on the server with systemctl status %s-daemon", build.Name)
		case <-ticker.C:
		}
	}
	updated, err := s.Get(actor.WorkspaceID, server.ID)
	if err != nil {
		return nil, err
	}
	// A daemon that already ran this version changed nothing worth a row.
	if updated.DaemonVersion != server.DaemonVersion {
		s.record(actor, "server.daemon_updated", "server", server.Name, map[string]string{"from": server.DaemonVersion, "to": updated.DaemonVersion})
	}
	return updated, nil
}

// AutoUpdateDaemons reports whether the hub updates the daemons of
// the workspace to its own version by itself.
func (s *ServerService) AutoUpdateDaemons(workspaceID string) (bool, error) {
	return s.servers.AutoUpdateDaemons(workspaceID)
}

// SetAutoUpdateDaemons turns updating the daemons by themselves on or off.
// Turned on, it updates the daemons that are behind right away.
func (s *ServerService) SetAutoUpdateDaemons(actor Actor, on bool) error {
	before, err := s.servers.AutoUpdateDaemons(actor.WorkspaceID)
	if err != nil {
		return err
	}
	if err := s.servers.SetAutoUpdateDaemons(actor.WorkspaceID, on); err != nil {
		return err
	}
	if before != on {
		s.record(actor, "workspace.daemon_updates", "workspace", "", map[string]string{"on": strconv.FormatBool(on)})
	}
	if on {
		s.mu.Lock()
		delete(s.autoHalted, actor.WorkspaceID)
		s.mu.Unlock()
		go s.autoUpdate(actor.WorkspaceID)
	}
	return nil
}

// UpdateDaemonsTo has the hub update every daemon older than
// version by itself, whenever a daemon connects. "" or a development build
// turns it off.
func (s *ServerService) UpdateDaemonsTo(version string) {
	s.mu.Lock()
	s.autoVersion = version
	s.mu.Unlock()
}

// DaemonBehind reports whether a daemon should update to the hub's
// release: it runs an older release, or a build without one, such as a
// commit like b4be272, which would otherwise never update.
func DaemonBehind(release, daemon string) bool {
	if parseVersion(release) == nil {
		return false
	}
	return parseVersion(daemon) == nil || NewerVersion(release, daemon)
}

// autoUpdate updates the daemons of a workspace that are behind, one at a
// time. A daemon that refuses, such as during a deploy, is tried again when
// it connects next. One that does not come back stops it for the workspace
// until someone updates a daemon by hand, and the activity says so.
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
	if on, err := s.servers.AutoUpdateDaemons(workspaceID); err != nil || !on {
		return
	}
	tried := map[string]bool{}
	for {
		servers, err := s.List(workspaceID)
		if err != nil {
			slog.Error("list servers to update their daemons", "error", err, "workspace_id", workspaceID)
			return
		}
		var next *model.Server
		for index, server := range servers {
			if server.Online && server.Rules == "" && !tried[server.ID] && DaemonBehind(version, server.DaemonVersion) {
				next = &servers[index]
				break
			}
		}
		if next == nil {
			return
		}
		tried[next.ID] = true
		_, err = s.UpdateDaemon(context.Background(), System(workspaceID), next.ID)
		var daemonErr *DaemonError
		var stepErr *StepError
		switch {
		case errors.As(err, &stepErr):
			// A step failed: it did not try again by itself, so apps it
			// stops do not go down and up on every connect.
			s.mu.Lock()
			s.autoHalted[workspaceID] = true
			s.mu.Unlock()
			return
		case err == nil, errors.As(err, &daemonErr), errors.Is(err, ErrServerOffline), errors.Is(err, ErrNoDaemonBinary), errors.Is(err, ErrDaemonUpdating):
			// Updated, or refused for now: the next connect tries again.
		default:
			s.mu.Lock()
			s.autoHalted[workspaceID] = true
			s.mu.Unlock()
			s.record(System(workspaceID), "server.daemon_update_failed", "server", next.Name, map[string]string{"from": next.DaemonVersion, "to": version, "error": clipText(err.Error(), 300)})
			return
		}
	}
}
