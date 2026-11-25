package model

import (
	"strings"
	"time"
)

// OAuthClient is an MCP client that registered itself (RFC 7591), such as
// Claude or Cursor. It is only a name and the places it may send the person
// back to. Nobody checked either: the approval page shows the name as the
// client's own word and the host it goes back to.
type OAuthClient struct {
	ID         string `db:"id"`
	ClientName string `db:"client_name"`
	// RedirectURIs are the URIs, one per line.
	RedirectURIs string     `db:"redirect_uris"`
	CreatedAt    time.Time  `db:"created_at"`
	LastUsedAt   *time.Time `db:"last_used_at"`
}

// Redirects are the client's redirect URIs.
func (c OAuthClient) Redirects() []string {
	return strings.Split(c.RedirectURIs, "\n")
}

// OAuthCodeLifetime is how long an authorization code works. The client
// trades it at once, so a minute is plenty.
const OAuthCodeLifetime = time.Minute

// OAuthCode is an authorization code: the person approved a connection,
// and the client trades the code for its tokens, once. Only its hash is
// stored, with what the client must show again.
type OAuthCode struct {
	ID           string `db:"id"`
	CodeHash     string `db:"code_hash"`
	ConnectionID string `db:"connection_id"`
	RedirectURI  string `db:"redirect_uri"`
	// CodeChallenge is the PKCE challenge (S256): the client proves with
	// the verifier that it is the one that asked.
	CodeChallenge string     `db:"code_challenge"`
	ExpiresAt     time.Time  `db:"expires_at"`
	UsedAt        *time.Time `db:"used_at"`
	CreatedAt     time.Time  `db:"created_at"`
}
