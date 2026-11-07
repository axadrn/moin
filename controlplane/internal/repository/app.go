package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	ErrAppNotFound      = errors.New("app not found")
	ErrDuplicateApp     = errors.New("app name already exists")
	ErrDeployNotFound   = errors.New("deploy not found")
	ErrRegistryNotFound = errors.New("registry not found")
	ErrNoBackupStorage  = errors.New("no backup storage")
	// ErrGitHubInstallationTaken means the installation is connected to
	// another workspace.
	ErrGitHubInstallationTaken = errors.New("github installation belongs to another workspace")
)

type AppRepository interface {
	Create(app *model.App) error
	ByIDOrNameForWorkspace(idOrName, workspaceID string) (*model.App, error)
	ByID(id string) (*model.App, error)
	ListByWorkspaceID(workspaceID string) ([]model.App, error)
	// SetPinned pins or unpins an app.
	SetPinned(appID string, on bool) error
	// LastDeploys is the time of each app's newest deploy, by app ID.
	LastDeploys(workspaceID string) (map[string]int64, error)
	DeleteForWorkspace(id, workspaceID string) error

	// CreateDeploy numbers the deploy after the newest one of its app.
	CreateDeploy(deploy *model.Deploy) error
	Deploy(appID string, number int) (*model.Deploy, error)
	DeployByID(id string) (*model.Deploy, error)
	ListDeploys(appID string, limit int) ([]model.Deploy, error)
	FinishDeploy(id, status, errorMessage, log, detected string, at time.Time) error

	Env(appID string) (map[string]string, error)
	SetEnv(appID string, set map[string]string, unset []string, at time.Time) error

	SetRouting(appID, domains, canonical string) error
	SetServiceDomains(appID, domains string) error
	SetInstant(appID, provider string, open bool) error
	SetInstantHost(appID, host string) error
	SetInstantPassword(appID, sealed string) error
	SetProtect(appID, protect string) error
	SetStopped(appID string, stopped bool) error
	SetCatalog(appID, slug string) error
	// CatalogApps are the apps of every workspace installed from the
	// catalog entry slug.
	CatalogApps(slug string) ([]model.App, error)
	SetCatalogNotified(appID, version string) error

	ByDeployHookHash(hash string) (*model.App, error)
	SetDeployHookHash(appID, hash string) error
	RemoveDeployHook(appID string) error

	BucketRepository
	// AddBackupMoment remembers a moment to go back to and forgets those
	// older than before.
	AddBackupMoment(moment model.BackupMoment, before time.Time) error
	// BackupMoments are the remembered moments of an app since a time,
	// newest first.
	BackupMoments(appID string, since time.Time) ([]model.BackupMoment, error)
	BackupStatus(appID string) ([]model.BackupStatus, error)
	SetBackupStatus(status model.BackupStatus) error

	GitHubInstallations(workspaceID string) ([]model.GitHubInstallation, error)
	AddGitHubInstallation(installation model.GitHubInstallation) error
	DeleteGitHubInstallation(installationID int64) error
	// GitHubInstallation returns an installation with the apps linked
	// through it.
	GitHubInstallation(installationID int64) (*model.GitHubInstallation, []model.App, error)
	SetRepo(appID, repo, branch string, autoDeploy bool, installationID int64) error
	// SetAutoDeploy turns deploy on push on or off for a linked app and
	// leaves its repo and branch alone.
	SetAutoDeploy(appID string, on bool) error
	AppsForPush(installationID int64, repo, branch string) ([]model.App, error)

	Registries(workspaceID string) ([]model.Registry, error)
	SetRegistry(registry model.Registry) error
	DeleteRegistry(workspaceID, host string) error

	UptimeRepository
	MetricsRepository
}

type appRepository struct {
	db *sqlx.DB
	// history is history.db: metrics and uptime counts, no backup; see
	// db.OpenHistory.
	history *sqlx.DB
}

func NewAppRepository(db, history *sqlx.DB) AppRepository {
	return &appRepository{db: db, history: history}
}

const appSelect = `
	SELECT a.*, s.name AS server_name, w.slug AS workspace_slug,
		COALESCE((SELECT name FROM buckets WHERE id = a.bucket_id), '') AS bucket_name,
		COALESCE((SELECT number FROM deploys WHERE app_id = a.id ORDER BY number DESC LIMIT 1), 0) AS latest_number,
		COALESCE((SELECT status FROM deploys WHERE app_id = a.id ORDER BY number DESC LIMIT 1), '') AS latest_status,
		COALESCE((SELECT number FROM deploys WHERE app_id = a.id AND status = 'succeeded' ORDER BY number DESC LIMIT 1), 0) AS live_number
	FROM apps a JOIN servers s ON s.id = a.server_id JOIN workspaces w ON w.id = a.workspace_id`

func (r *appRepository) Create(app *model.App) error {
	_, err := r.db.Exec(`INSERT INTO apps (id, workspace_id, server_id, name, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		app.ID, app.WorkspaceID, app.ServerID, app.Name, app.CreatedAt)
	if err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed: apps.workspace_id, apps.name") || strings.Contains(err.Error(), "duplicate key value")) {
		return ErrDuplicateApp
	}
	return err
}

func (r *appRepository) get(query string, args ...any) (*model.App, error) {
	app := &model.App{}
	err := r.db.Get(app, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAppNotFound
	}
	if err != nil {
		return nil, err
	}
	return app, nil
}

func (r *appRepository) ByIDOrNameForWorkspace(idOrName, workspaceID string) (*model.App, error) {
	return r.get(appSelect+` WHERE a.workspace_id = $1 AND (a.id = $2 OR a.name = $2)`, workspaceID, idOrName)
}

func (r *appRepository) ByID(id string) (*model.App, error) {
	return r.get(appSelect+` WHERE a.id = $1`, id)
}

func (r *appRepository) ListByWorkspaceID(workspaceID string) ([]model.App, error) {
	apps := []model.App{}
	err := r.db.Select(&apps, appSelect+` WHERE a.workspace_id = $1 ORDER BY a.name, a.id`, workspaceID)
	return apps, err
}

func (r *appRepository) DeleteForWorkspace(id, workspaceID string) error {
	result, err := r.db.Exec(`DELETE FROM apps WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrAppNotFound
	}
	return nil
}

func (r *appRepository) CreateDeploy(deploy *model.Deploy) error {
	return r.db.QueryRow(`
		INSERT INTO deploys (id, app_id, number, status, source_key, source_sha256, from_number, image, ref, commit_sha, commit_message, created_at)
		VALUES ($1, $2, (SELECT COALESCE(MAX(number), 0) + 1 FROM deploys WHERE app_id = $2), $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING number
	`, deploy.ID, deploy.AppID, deploy.Status, deploy.SourceKey, deploy.SourceSHA256, deploy.FromNumber, deploy.Image, deploy.Ref, deploy.CommitSHA, deploy.CommitMessage, deploy.CreatedAt).Scan(&deploy.Number)
}

func (r *appRepository) getDeploy(query string, args ...any) (*model.Deploy, error) {
	deploy := &model.Deploy{}
	err := r.db.Get(deploy, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeployNotFound
	}
	if err != nil {
		return nil, err
	}
	return deploy, nil
}

func (r *appRepository) Deploy(appID string, number int) (*model.Deploy, error) {
	return r.getDeploy(`SELECT * FROM deploys WHERE app_id = $1 AND number = $2`, appID, number)
}

func (r *appRepository) DeployByID(id string) (*model.Deploy, error) {
	return r.getDeploy(`SELECT * FROM deploys WHERE id = $1`, id)
}

// ListDeploys returns the newest deploys first, without their logs.
func (r *appRepository) ListDeploys(appID string, limit int) ([]model.Deploy, error) {
	deploys := []model.Deploy{}
	err := r.db.Select(&deploys, `
		SELECT id, app_id, number, status, source_key, source_sha256, from_number, image, ref, commit_sha, commit_message, error, '' AS log, detected, created_at, finished_at
		FROM deploys WHERE app_id = $1 ORDER BY number DESC LIMIT $2
	`, appID, limit)
	return deploys, err
}

// FinishDeploy ends a running deploy. A deploy that already ended stays as it is.
func (r *appRepository) FinishDeploy(id, status, errorMessage, log, detected string, at time.Time) error {
	_, err := r.db.Exec(`
		UPDATE deploys SET status = $1, error = $2, log = $3, detected = CASE WHEN $4 = '' THEN detected ELSE $4 END, finished_at = $5
		WHERE id = $6 AND status = 'running'
	`, status, errorMessage, log, detected, at, id)
	return err
}

func (r *appRepository) Env(appID string) (map[string]string, error) {
	rows := []struct {
		Key   string `db:"key"`
		Value string `db:"value"`
	}{}
	err := r.db.Select(&rows, `SELECT key, value FROM app_env WHERE app_id = $1`, appID)
	if err != nil {
		return nil, err
	}
	env := make(map[string]string, len(rows))
	for _, row := range rows {
		env[row.Key] = row.Value
	}
	return env, nil
}

// SetEnv writes and removes env vars in one transaction.
func (r *appRepository) SetEnv(appID string, set map[string]string, unset []string, at time.Time) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range set {
		_, err = tx.Exec(`
			INSERT INTO app_env (app_id, key, value, updated_at) VALUES ($1, $2, $3, $4)
			ON CONFLICT (app_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, appID, key, value, at)
		if err != nil {
			return err
		}
	}
	for _, key := range unset {
		_, err = tx.Exec(`DELETE FROM app_env WHERE app_id = $1 AND key = $2`, appID, key)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *appRepository) ByDeployHookHash(hash string) (*model.App, error) {
	return r.get(appSelect+` WHERE a.deploy_hook_hash = $1`, hash)
}

// SetDeployHookHash replaces the deploy hook of an app; the old URL stops working.
func (r *appRepository) SetDeployHookHash(appID, hash string) error {
	_, err := r.db.Exec(`UPDATE apps SET deploy_hook_hash = $1 WHERE id = $2`, hash, appID)
	return err
}

// RemoveDeployHook turns the deploy hook of an app off; its URL stops working.
func (r *appRepository) RemoveDeployHook(appID string) error {
	_, err := r.db.Exec(`UPDATE apps SET deploy_hook_hash = NULL WHERE id = $1`, appID)
	return err
}

func (r *appRepository) Registries(workspaceID string) ([]model.Registry, error) {
	registries := []model.Registry{}
	err := r.db.Select(&registries, `SELECT * FROM registries WHERE workspace_id = $1 ORDER BY host`, workspaceID)
	return registries, err
}

func (r *appRepository) SetRegistry(registry model.Registry) error {
	_, err := r.db.Exec(`
		INSERT INTO registries (workspace_id, host, username, password, created_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id, host) DO UPDATE SET username = excluded.username, password = excluded.password
	`, registry.WorkspaceID, registry.Host, registry.Username, registry.Password, registry.CreatedAt)
	return err
}

func (r *appRepository) DeleteRegistry(workspaceID, host string) error {
	result, err := r.db.Exec(`DELETE FROM registries WHERE workspace_id = $1 AND host = $2`, workspaceID, host)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrRegistryNotFound
	}
	return nil
}

// SetRouting stores the domains (a JSON array) and the canonical domain.
func (r *appRepository) SetRouting(appID, domains, canonical string) error {
	_, err := r.db.Exec(`UPDATE apps SET domains = $1, canonical = $2 WHERE id = $3`, domains, canonical, appID)
	return err
}

// SetServiceDomains stores the domains of the other services, a JSON object.
func (r *appRepository) SetServiceDomains(appID, domains string) error {
	_, err := r.db.Exec(`UPDATE apps SET service_domains = $1 WHERE id = $2`, domains, appID)
	return err
}

// SetInstant stores the instant address provider and whether it is open.
func (r *appRepository) SetInstant(appID, provider string, open bool) error {
	_, err := r.db.Exec(`UPDATE apps SET instant = $1, instant_open = $2 WHERE id = $3`, provider, open, appID)
	return err
}

// SetInstantHost stores the instant address the app is served at.
func (r *appRepository) SetInstantHost(appID, host string) error {
	_, err := r.db.Exec(`UPDATE apps SET instant_host = $1 WHERE id = $2`, host, appID)
	return err
}

// SetInstantPassword stores the sealed password moin made for the instant
// address.
func (r *appRepository) SetInstantPassword(appID, sealed string) error {
	_, err := r.db.Exec(`UPDATE apps SET instant_password = $1 WHERE id = $2`, sealed, appID)
	return err
}

// SetProtect stores the htpasswd line, or clears it.
func (r *appRepository) SetProtect(appID, protect string) error {
	_, err := r.db.Exec(`UPDATE apps SET protect = $1 WHERE id = $2`, protect, appID)
	return err
}

// SetStopped marks the app as stopped by its owner, or as running again.
func (r *appRepository) CatalogApps(slug string) ([]model.App, error) {
	apps := []model.App{}
	err := r.db.Select(&apps, appSelect+` WHERE a.catalog = $1 ORDER BY a.created_at`, slug)
	return apps, err
}

func (r *appRepository) SetCatalogNotified(appID, version string) error {
	_, err := r.db.Exec(`UPDATE apps SET catalog_notified = $1 WHERE id = $2`, version, appID)
	return err
}

func (r *appRepository) SetCatalog(appID, slug string) error {
	_, err := r.db.Exec(`UPDATE apps SET catalog = $1 WHERE id = $2`, slug, appID)
	return err
}

func (r *appRepository) SetStopped(appID string, stopped bool) error {
	_, err := r.db.Exec(`UPDATE apps SET stopped = $1 WHERE id = $2`, stopped, appID)
	return err
}

func (r *appRepository) AddBackupMoment(moment model.BackupMoment, before time.Time) error {
	_, err := r.db.Exec(`INSERT INTO backup_moments (app_id, database, kind, detail, at) VALUES ($1, $2, $3, $4, $5)`,
		moment.AppID, moment.Database, moment.Kind, moment.Detail, moment.At.UTC())
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`DELETE FROM backup_moments WHERE app_id = $1 AND at < $2`, moment.AppID, before.UTC())
	return err
}

func (r *appRepository) BackupMoments(appID string, since time.Time) ([]model.BackupMoment, error) {
	moments := []model.BackupMoment{}
	err := r.db.Select(&moments, `SELECT * FROM backup_moments WHERE app_id = $1 AND at >= $2 ORDER BY at DESC, id DESC`, appID, since.UTC())
	return moments, err
}

func (r *appRepository) BackupStatus(appID string) ([]model.BackupStatus, error) {
	statuses := []model.BackupStatus{}
	err := r.db.Select(&statuses, `SELECT * FROM backup_status WHERE app_id = $1 ORDER BY database`, appID)
	return statuses, err
}

func (r *appRepository) SetBackupStatus(status model.BackupStatus) error {
	_, err := r.db.Exec(`
		INSERT INTO backup_status (app_id, database, since, replicated_at, verified_at, error, updated_at, drill_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (app_id, database) DO UPDATE SET since = excluded.since, replicated_at = excluded.replicated_at,
			verified_at = COALESCE(excluded.verified_at, backup_status.verified_at), error = excluded.error, updated_at = excluded.updated_at,
			drill_error = excluded.drill_error
	`, status.AppID, status.Database, status.Since, status.ReplicatedAt, status.VerifiedAt, status.Error, status.UpdatedAt, status.DrillError)
	return err
}

func (r *appRepository) GitHubInstallations(workspaceID string) ([]model.GitHubInstallation, error) {
	installations := []model.GitHubInstallation{}
	err := r.db.Select(&installations, `SELECT * FROM github_installations WHERE workspace_id = $1 ORDER BY account`, workspaceID)
	return installations, err
}

// AddGitHubInstallation stores an installation for a workspace. One that is
// connected to another workspace stays there: ErrGitHubInstallationTaken.
func (r *appRepository) AddGitHubInstallation(installation model.GitHubInstallation) error {
	result, err := r.db.Exec(`
		INSERT INTO github_installations (installation_id, workspace_id, account, created_at, settings_url) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (installation_id) DO UPDATE SET account = excluded.account, settings_url = excluded.settings_url
		WHERE github_installations.workspace_id = excluded.workspace_id
	`, installation.InstallationID, installation.WorkspaceID, installation.Account, installation.CreatedAt, installation.SettingsURL)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrGitHubInstallationTaken
	}
	return nil
}

// DeleteGitHubInstallation forgets an installation and unlinks the apps
// that followed one of its repos.
func (r *appRepository) GitHubInstallation(installationID int64) (*model.GitHubInstallation, []model.App, error) {
	installation := &model.GitHubInstallation{}
	err := r.db.Get(installation, `SELECT * FROM github_installations WHERE installation_id = $1`, installationID)
	if err != nil {
		return nil, nil, err
	}
	apps := []model.App{}
	err = r.db.Select(&apps, appSelect+` WHERE a.installation_id = $1`, installationID)
	return installation, apps, err
}

func (r *appRepository) DeleteGitHubInstallation(installationID int64) error {
	_, err := r.db.Exec(`UPDATE apps SET repo = '', branch = '', auto_deploy = 0, installation_id = 0 WHERE installation_id = $1`, installationID)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`DELETE FROM github_installations WHERE installation_id = $1`, installationID)
	return err
}

func (r *appRepository) SetRepo(appID, repo, branch string, autoDeploy bool, installationID int64) error {
	_, err := r.db.Exec(`UPDATE apps SET repo = $1, branch = $2, auto_deploy = $3, installation_id = $4 WHERE id = $5`, repo, branch, autoDeploy, installationID, appID)
	return err
}

func (r *appRepository) SetAutoDeploy(appID string, on bool) error {
	_, err := r.db.Exec(`UPDATE apps SET auto_deploy = $1 WHERE id = $2 AND repo != ''`, on, appID)
	return err
}

func (r *appRepository) AppsForPush(installationID int64, repo, branch string) ([]model.App, error) {
	apps := []model.App{}
	err := r.db.Select(&apps, appSelect+` WHERE a.installation_id = $1 AND lower(a.repo) = lower($2) AND a.branch = $3 AND a.auto_deploy = 1`, installationID, repo, branch)
	return apps, err
}

func (r *appRepository) SetPinned(appID string, on bool) error {
	_, err := r.db.Exec(`UPDATE apps SET pinned = $1 WHERE id = $2`, on, appID)
	return err
}

func (r *appRepository) LastDeploys(workspaceID string) (map[string]int64, error) {
	var rows []struct {
		AppID     string    `db:"app_id"`
		CreatedAt time.Time `db:"created_at"`
	}
	err := r.db.Select(&rows, `SELECT d.app_id, d.created_at FROM deploys d JOIN apps a ON a.id = d.app_id
		WHERE a.workspace_id = $1 AND d.number = (SELECT MAX(number) FROM deploys WHERE app_id = d.app_id)`, workspaceID)
	if err != nil {
		return nil, err
	}
	last := make(map[string]int64, len(rows))
	for _, row := range rows {
		last[row.AppID] = row.CreatedAt.Unix()
	}
	return last, nil
}
