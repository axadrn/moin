package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	ErrAlertDestinationNotFound  = errors.New("alert destination not found")
	ErrDuplicateAlertDestination = errors.New("alert destination name already exists")
)

// AlertDestinations stores where the alerts of each workspace go.
type AlertDestinations struct {
	db *sqlx.DB
}

func NewAlertDestinations(db *sqlx.DB) *AlertDestinations {
	return &AlertDestinations{db: db}
}

// List returns the destinations of a workspace by name.
func (r *AlertDestinations) List(workspaceID string) ([]model.AlertDestination, error) {
	destinations := []model.AlertDestination{}
	err := r.db.Select(&destinations, `SELECT * FROM alert_destinations WHERE workspace_id = $1 ORDER BY name`, workspaceID)
	return destinations, err
}

// Count returns how many destinations a workspace has.
func (r *AlertDestinations) Count(workspaceID string) (int, error) {
	var count int
	err := r.db.Get(&count, `SELECT COUNT(*) FROM alert_destinations WHERE workspace_id = $1`, workspaceID)
	return count, err
}

// ByIDOrName finds a destination of a workspace by its ID or name.
func (r *AlertDestinations) ByIDOrName(workspaceID, idOrName string) (*model.AlertDestination, error) {
	var destination model.AlertDestination
	err := r.db.Get(&destination, `SELECT * FROM alert_destinations WHERE workspace_id = $1 AND (id = $2 OR name = $2)`, workspaceID, idOrName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAlertDestinationNotFound
	}
	return &destination, err
}

// Create stores a new destination.
func (r *AlertDestinations) Create(destination model.AlertDestination) error {
	_, err := r.db.Exec(`
		INSERT INTO alert_destinations (id, workspace_id, name, kind, target, chat_id, hint, secret, events, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, destination.ID, destination.WorkspaceID, destination.Name, destination.Kind, destination.Target, destination.ChatID, destination.Hint, destination.Secret, destination.Events, destination.CreatedAt)
	if err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed: alert_destinations.workspace_id, alert_destinations.name") || strings.Contains(err.Error(), "duplicate key value")) {
		return ErrDuplicateAlertDestination
	}
	return err
}

// Delete removes a destination of a workspace.
func (r *AlertDestinations) Delete(workspaceID, id string) error {
	result, err := r.db.Exec(`DELETE FROM alert_destinations WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrAlertDestinationNotFound
	}
	return nil
}

// SetResult stores how a send went: an empty failure clears the failing
// state, a failure keeps the time it began.
func (r *AlertDestinations) SetResult(id, failure string, at time.Time) error {
	if failure == "" {
		_, err := r.db.Exec(`UPDATE alert_destinations SET failing_since = NULL, last_error = '', last_sent_at = $1 WHERE id = $2`, at, id)
		return err
	}
	_, err := r.db.Exec(`UPDATE alert_destinations SET failing_since = COALESCE(failing_since, $1), last_error = $2 WHERE id = $3`, at, failure, id)
	return err
}
