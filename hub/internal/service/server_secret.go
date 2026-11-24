package service

// Secrets live only on the servers. The hub forwards a value once to the
// daemon, which keeps env values in apps/<app>/app.env and the secrets of
// the server in server.json, and keeps only names and dates: a server
// that is offline cannot take a value, and the hub keeps none for it.

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

var (
	// ErrNoServer is a secret for a workspace without a server: no one
	// could keep it.
	ErrNoServer = errors.New("add a server first: a secret lives on a server")
	// ErrServerRequired is a secret for a workspace with several servers
	// that does not name the one to keep it.
	ErrServerRequired = errors.New("name the server that keeps it: this workspace has more than one")
)

// OfflineError refuses a change that needs a server that is offline: the
// hub keeps no value to send later.
type OfflineError struct{ Server string }

func (e *OfflineError) Error() string {
	return e.Server + " is offline. Try again when it is back."
}

// Is lets errors.Is(err, ErrServerOffline) see it.
func (e *OfflineError) Is(target error) bool { return target == ErrServerOffline }

// MissingSecretError is a server that lacks a secret a request needs, in
// words that say where to enter it.
type MissingSecretError struct{ Message string }

func (e *MissingSecretError) Error() string { return e.Message }

// SecretOn is a server that keeps a secret, and since when.
type SecretOn struct {
	Server    string
	UpdatedAt time.Time
}

// SecretServers says where a secret of the workspace is: On the servers
// that keep it, and MissingOn, the names of the servers that do not.
type SecretServers struct {
	On        []SecretOn
	MissingOn []string
}

// secretsIndex is which server of a workspace keeps which secret.
type secretsIndex struct {
	servers []model.Server
	kept    map[string]map[string]time.Time
}

func (s *ServerService) secretsIndex(workspaceID string) (secretsIndex, error) {
	servers, err := s.servers.ListByWorkspaceID(workspaceID)
	if err != nil {
		return secretsIndex{}, err
	}
	secrets, err := s.servers.ServerSecrets(workspaceID)
	if err != nil {
		return secretsIndex{}, err
	}
	index := secretsIndex{servers: servers, kept: map[string]map[string]time.Time{}}
	for _, secret := range secrets {
		if index.kept[secret.ServerID] == nil {
			index.kept[secret.ServerID] = map[string]time.Time{}
		}
		index.kept[secret.ServerID][secret.Name] = secret.UpdatedAt
	}
	return index, nil
}

// has says whether the server keeps the secret name.
func (i secretsIndex) has(serverID, name string) bool {
	_, ok := i.kept[serverID][name]
	return ok
}

// of says which servers keep the secret name.
func (i secretsIndex) of(name string) SecretServers {
	where := SecretServers{On: []SecretOn{}, MissingOn: []string{}}
	for _, server := range i.servers {
		if at, ok := i.kept[server.ID][name]; ok {
			where.On = append(where.On, SecretOn{Server: server.Name, UpdatedAt: at})
		} else {
			where.MissingOn = append(where.MissingOn, server.Name)
		}
	}
	return where
}

// secretServer is the online server a secret goes to: the one named, or
// the workspace's only server.
func (s *ServerService) secretServer(workspaceID, name string) (*model.Server, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		servers, err := s.servers.ListByWorkspaceID(workspaceID)
		if err != nil {
			return nil, err
		}
		switch len(servers) {
		case 0:
			return nil, ErrNoServer
		case 1:
			name = servers[0].ID
		default:
			return nil, ErrServerRequired
		}
	}
	server, err := s.servers.ByIDOrNameForWorkspace(name, workspaceID)
	if err != nil {
		return nil, err
	}
	if !s.Online(server.ID) {
		return nil, &OfflineError{Server: server.Name}
	}
	return server, nil
}

// secretTimeout is how long the hub waits for a daemon to keep a secret.
const secretTimeout = 30 * time.Second

// setSecret hands a server a secret to keep, or has it remove one when
// value is empty, and notes that it keeps it. The value goes nowhere else.
func (s *ServerService) setSecret(ctx context.Context, server *model.Server, name, value string) error {
	ctx, cancel := context.WithTimeout(ctx, secretTimeout)
	defer cancel()
	err := s.Request(ctx, server.ID, protocol.TypeSecretSet, protocol.SecretSet{Name: name, Value: value}, nil)
	if errors.Is(err, ErrServerOffline) {
		return &OfflineError{Server: server.Name}
	}
	if err != nil {
		return err
	}
	return s.servers.SetServerSecret(server.ID, name, value == "", s.now().UTC())
}

// dropSecret has every online server of the workspace that keeps the
// secret name remove it, and forgets it for all. An offline one removes it
// when it connects (forgetOldSecrets).
func (s *ServerService) dropSecret(ctx context.Context, workspaceID, name string) {
	index, err := s.secretsIndex(workspaceID)
	if err != nil {
		slog.Error("list server secrets failed", "workspace_id", workspaceID, "error", err)
		return
	}
	for _, server := range index.servers {
		if !index.has(server.ID, name) {
			continue
		}
		if s.Online(server.ID) {
			if err := s.setSecret(ctx, &server, name, ""); err != nil {
				slog.Warn("remove a secret from a server failed", "server_id", server.ID, "secret", name, "error", err)
			}
			continue
		}
		if err := s.servers.SetServerSecret(server.ID, name, true, s.now().UTC()); err != nil {
			slog.Error("forget a server secret failed", "server_id", server.ID, "error", err)
		}
	}
}

// keepSecretNames stores the names of the secrets a daemon reported in
// its hello: what the server keeps is the truth, also after a rebuild.
func (s *ServerService) keepSecretNames(serverID string, names []string, at time.Time) {
	clean := []string{}
	for _, name := range names {
		if len(clean) < 500 && name != "" && len(name) <= 300 && !slices.Contains(clean, name) {
			clean = append(clean, name)
		}
	}
	if err := s.servers.KeepServerSecrets(serverID, clean, at); err != nil {
		slog.Error("store the secrets a daemon keeps failed", "server_id", serverID, "error", err)
	}
}

// forgetOldSecrets has a server that just connected remove the keys of
// buckets and the logins of registries the workspace no longer has, which
// were removed while it was offline.
func (s *AppService) forgetOldSecrets(serverID, workspaceID string) {
	index, err := s.servers.secretsIndex(workspaceID)
	if err != nil {
		return
	}
	buckets, err := s.apps.Buckets(workspaceID)
	if err != nil {
		return
	}
	registries, err := s.apps.Registries(workspaceID)
	if err != nil {
		return
	}
	for name := range index.kept[serverID] {
		kind, of := protocol.SecretOf(name)
		gone := kind == "bucket" && !slices.ContainsFunc(buckets, func(bucket model.Bucket) bool { return bucket.ID == of }) ||
			kind == "registry" && !slices.ContainsFunc(registries, func(registry model.Registry) bool { return registry.Host == of })
		if !gone {
			continue
		}
		server, err := s.servers.servers.ByIDOrNameForWorkspace(serverID, workspaceID)
		if err != nil {
			return
		}
		if err := s.servers.setSecret(context.Background(), server, name, ""); err != nil {
			slog.Warn("remove an old secret from a server failed", "server_id", serverID, "secret", name, "error", err)
		}
	}
}
