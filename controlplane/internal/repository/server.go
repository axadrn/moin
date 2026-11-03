package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	ErrServerNotFound  = errors.New("server not found")
	ErrDuplicateServer = errors.New("server name already exists")
	ErrServerHasApps   = errors.New("server still has apps")
)

type ServerRepository interface {
	Create(server *model.Server) error
	ByIDOrNameForWorkspace(idOrName, workspaceID string) (*model.Server, error)
	ByInstallTokenHash(hash string) (*model.Server, error)
	ByAgentTokenHash(hash string) (*model.Server, error)
	ListByWorkspaceID(workspaceID string) ([]model.Server, error)
	Register(id, agentTokenHash string, hello model.Server) error
	UpdateHello(id string, hello model.Server) error
	TouchLastSeen(id string, at time.Time) error
	DeleteForWorkspace(id, workspaceID string) error
	DeleteWithApps(id, workspaceID string) (apps []string, err error)
	Rename(id, workspaceID, name string) error
	SetInstallToken(id, workspaceID, hash string, expiresAt time.Time) error
	SetSecurity(server model.Server) error
	SetSecurityReport(id, report string) error
	Integrations(workspaceID string) (model.Integrations, error)
	SetIntegrations(integrations model.Integrations) error
}

type serverRepository struct {
	db *sqlx.DB
}

func NewServerRepository(db *sqlx.DB) ServerRepository {
	return &serverRepository{db: db}
}

func (r *serverRepository) Create(server *model.Server) error {
	_, err := r.db.Exec(`
		INSERT INTO servers (id, workspace_id, name, install_token_hash, install_expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, server.ID, server.WorkspaceID, server.Name, server.InstallTokenHash, server.InstallExpiresAt, server.CreatedAt)
	if err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed: servers.workspace_id, servers.name") || strings.Contains(err.Error(), "duplicate key value")) {
		return ErrDuplicateServer
	}
	return err
}

func (r *serverRepository) get(query string, args ...any) (*model.Server, error) {
	server := &model.Server{}
	err := r.db.Get(server, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return server, nil
}

// ByIDOrNameForWorkspace finds a server of the user by its ID or its name, so the
// CLI can say `ops servers rm prod`.
func (r *serverRepository) ByIDOrNameForWorkspace(idOrName, workspaceID string) (*model.Server, error) {
	return r.get(`SELECT * FROM servers WHERE workspace_id = $1 AND (id = $2 OR name = $2)`, workspaceID, idOrName)
}

func (r *serverRepository) ByInstallTokenHash(hash string) (*model.Server, error) {
	return r.get(`SELECT * FROM servers WHERE install_token_hash = $1`, hash)
}

func (r *serverRepository) ByAgentTokenHash(hash string) (*model.Server, error) {
	return r.get(`SELECT * FROM servers WHERE agent_token_hash = $1`, hash)
}

// ListByWorkspaceID returns the servers of a user by name.
func (r *serverRepository) ListByWorkspaceID(workspaceID string) ([]model.Server, error) {
	servers := []model.Server{}
	err := r.db.Select(&servers, `SELECT * FROM servers WHERE workspace_id = $1 ORDER BY name, id`, workspaceID)
	return servers, err
}

// Register clears the install token, so it works once, and stores the agent
// token and what the agent said about its server.
func (r *serverRepository) Register(id, agentTokenHash string, hello model.Server) error {
	_, err := r.db.Exec(`
		UPDATE servers
		SET install_token_hash = NULL, install_expires_at = NULL, agent_token_hash = $1,
			hostname = $2, os = $3, arch = $4, agent_version = $5, rules = $6
		WHERE id = $7
	`, agentTokenHash, hello.Hostname, hello.OS, hello.Arch, hello.AgentVersion, hello.Rules, id)
	return err
}

func (r *serverRepository) UpdateHello(id string, hello model.Server) error {
	_, err := r.db.Exec(`
		UPDATE servers SET hostname = $1, os = $2, arch = $3, agent_version = $4, last_seen_at = $5, address = $6, rules = $7, addresses = $8 WHERE id = $9
	`, hello.Hostname, hello.OS, hello.Arch, hello.AgentVersion, hello.LastSeenAt, hello.Address, hello.Rules, hello.Addresses, id)
	return err
}

func (r *serverRepository) TouchLastSeen(id string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE servers SET last_seen_at = $1 WHERE id = $2`, at, id)
	return err
}

// DeleteForWorkspace deletes a server. Another user's server is not found.
func (r *serverRepository) DeleteForWorkspace(id, workspaceID string) error {
	result, err := r.db.Exec(`DELETE FROM servers WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		return ErrServerHasApps
	}
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrServerNotFound
	}
	return nil
}

// DeleteWithApps deletes a server and its apps in one go and returns the
// names of the apps. Another user's server is not found.
func (r *serverRepository) DeleteWithApps(id, workspaceID string) ([]string, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var apps []string
	err = tx.Select(&apps, `SELECT name FROM apps WHERE server_id = $1 AND workspace_id = $2 ORDER BY name`, id, workspaceID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`DELETE FROM apps WHERE server_id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec(`DELETE FROM servers WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return nil, err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return nil, ErrServerNotFound
	}
	return apps, tx.Commit()
}

// Rename gives a server a new name, unique in its workspace.
func (r *serverRepository) Rename(id, workspaceID, name string) error {
	result, err := r.db.Exec(`UPDATE servers SET name = $1 WHERE id = $2 AND workspace_id = $3`, name, id, workspaceID)
	if err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed: servers.workspace_id, servers.name") || strings.Contains(err.Error(), "duplicate key value")) {
		return ErrDuplicateServer
	}
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrServerNotFound
	}
	return nil
}

// SetInstallToken replaces the install token of a server; the old one
// stops working.
func (r *serverRepository) SetInstallToken(id, workspaceID, hash string, expiresAt time.Time) error {
	result, err := r.db.Exec(`UPDATE servers SET install_token_hash = $1, install_expires_at = $2 WHERE id = $3 AND workspace_id = $4`, hash, expiresAt, id, workspaceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrServerNotFound
	}
	return nil
}

func (r *serverRepository) SetSecurity(server model.Server) error {
	_, err := r.db.Exec(`UPDATE servers SET tunnel = $1, tunnel_id = $2, tunnel_token = $3, ssh_tailscale = $4, firewall = $5 WHERE id = $6`,
		server.Tunnel, server.TunnelID, server.TunnelToken, server.SSHTailscale, server.Firewall, server.ID)
	return err
}

func (r *serverRepository) SetSecurityReport(id, report string) error {
	_, err := r.db.Exec(`UPDATE servers SET security = $1 WHERE id = $2`, report, id)
	return err
}

// Integrations returns the user's keys; a user without any gets empty ones.
func (r *serverRepository) Integrations(workspaceID string) (model.Integrations, error) {
	integrations := model.Integrations{WorkspaceID: workspaceID}
	err := r.db.Get(&integrations, `SELECT * FROM integrations WHERE workspace_id = $1`, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return integrations, nil
	}
	return integrations, err
}

func (r *serverRepository) SetIntegrations(integrations model.Integrations) error {
	_, err := r.db.Exec(`
		INSERT INTO integrations (workspace_id, cloudflare_token, cloudflare_account, tailscale_key, updated_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id) DO UPDATE SET cloudflare_token = excluded.cloudflare_token, cloudflare_account = excluded.cloudflare_account,
			tailscale_key = excluded.tailscale_key, updated_at = excluded.updated_at
	`, integrations.WorkspaceID, integrations.CloudflareToken, integrations.CloudflareAccount, integrations.TailscaleKey, integrations.UpdatedAt)
	return err
}
