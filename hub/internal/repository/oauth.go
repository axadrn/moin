package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	ErrOAuthClientNotFound = errors.New("oauth client not found")
	ErrOAuthCodeNotFound   = errors.New("oauth code not found")
	// ErrOAuthCodeUsed is a code that was traded already.
	ErrOAuthCodeUsed = errors.New("oauth code used already")
)

// OAuth stores the MCP clients that registered themselves and their
// authorization codes.
type OAuth struct {
	db *sqlx.DB
}

func NewOAuth(db *sqlx.DB) *OAuth {
	return &OAuth{db: db}
}

// CreateClient stores a client that registered itself.
func (r *OAuth) CreateClient(client model.OAuthClient) error {
	_, err := r.db.Exec(`
		INSERT INTO oauth_clients (id, client_name, redirect_uris, created_at)
		VALUES ($1, $2, $3, $4)
	`, client.ID, client.ClientName, client.RedirectURIs, client.CreatedAt)
	return err
}

// Client finds a client by its client_id.
func (r *OAuth) Client(id string) (*model.OAuthClient, error) {
	var client model.OAuthClient
	err := r.db.Get(&client, `SELECT * FROM oauth_clients WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOAuthClientNotFound
	}
	if err != nil {
		return nil, err
	}
	return &client, nil
}

// ClientUsed records that a person approved a connection for the client.
func (r *OAuth) ClientUsed(id string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE oauth_clients SET last_used_at = $1 WHERE id = $2`, at, id)
	return err
}

// DeleteIdleClients removes the clients nobody needs: never used since
// before unused, or not used since before idle, and no live connection of
// them left.
func (r *OAuth) DeleteIdleClients(unused, idle time.Time) error {
	_, err := r.db.Exec(`
		DELETE FROM oauth_clients
		WHERE ((last_used_at IS NULL AND created_at < $1) OR last_used_at < $2)
		AND NOT EXISTS (SELECT 1 FROM connections c WHERE c.client_id = oauth_clients.id AND c.ended_at IS NULL)
	`, unused, idle)
	return err
}

// CreateCode stores an authorization code.
func (r *OAuth) CreateCode(code model.OAuthCode) error {
	_, err := r.db.Exec(`
		INSERT INTO oauth_codes (id, code_hash, connection_id, redirect_uri, code_challenge, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, code.ID, code.CodeHash, code.ConnectionID, code.RedirectURI, code.CodeChallenge, code.ExpiresAt, code.CreatedAt)
	return err
}

// Code finds an authorization code by its hash, used or not.
func (r *OAuth) Code(hash string) (*model.OAuthCode, error) {
	var code model.OAuthCode
	err := r.db.Get(&code, `SELECT * FROM oauth_codes WHERE code_hash = $1`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOAuthCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	return &code, nil
}

// UseCode marks a code traded. Only the first of two trades at once gets
// it; the second hears ErrOAuthCodeUsed.
func (r *OAuth) UseCode(id string, at time.Time) error {
	result, err := r.db.Exec(`UPDATE oauth_codes SET used_at = $1 WHERE id = $2 AND used_at IS NULL`, at, id)
	return oneRowOr(result, err, ErrOAuthCodeUsed)
}

// ExpiredUnusedCodes returns the codes nobody traded in time whose
// connection is still live.
func (r *OAuth) ExpiredUnusedCodes(now time.Time) ([]model.OAuthCode, error) {
	codes := []model.OAuthCode{}
	err := r.db.Select(&codes, `
		SELECT o.* FROM oauth_codes o JOIN connections c ON c.id = o.connection_id
		WHERE o.used_at IS NULL AND o.expires_at < $1 AND c.ended_at IS NULL
	`, now)
	return codes, err
}
