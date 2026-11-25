package model

import "time"

// Connection is a client a person let in through the browser: the CLI or
// the TUI of one computer (the device flow), or an AI agent (OAuth, with a
// ClientID). It acts for the person in one workspace with the rights they
// gave it, and its tokens hang off it. Removing it ends every token at once.
type Connection struct {
	ID          string `db:"id"`
	UserID      string `db:"user_id"`
	WorkspaceID string `db:"workspace_id"`
	// ClientName is the name the client gave, such as "moin CLI on
	// laptop-1". It is the client's word, so pages show it as untrusted
	// text.
	ClientName string `db:"client_name"`
	Rights     Rights `db:"rights"`
	// ClientID is the OAuth client of an AI agent; empty for the device
	// flow. A refresh must name the same client.
	ClientID string `db:"client_id"`
	// Resource is the one place its tokens work, such as "/mcp" for an AI
	// agent; empty for the API and MCP.
	Resource string `db:"resource"`
	// RedirectHost is where an AI agent sends the person back, like
	// claude.ai; empty for the device flow. Unlike the name, the hub
	// checked it: it is the host of the redirect URI.
	RedirectHost string     `db:"redirect_host"`
	CreatedAt    time.Time  `db:"created_at"`
	LastUsedAt   *time.Time `db:"last_used_at"`
	EndedAt      *time.Time `db:"ended_at"`
}

// Label names the connection for people: the client's name, and for an AI
// agent where it sends the person back, like "Claude · claude.ai".
func (c Connection) Label() string {
	if c.RedirectHost == "" {
		return c.ClientName
	}
	return c.ClientName + " · " + c.RedirectHost
}

// ConnectionIdle is how long a connection lives unused. Every use starts
// it again.
const ConnectionIdle = 90 * 24 * time.Hour

const (
	// AccessTokenLifetime is how long an access token of a connection
	// works.
	AccessTokenLifetime = time.Hour
	// RefreshTokenLifetime is how long a refresh token can be exchanged.
	// It is the idle time of the connection: the connection ends anyway
	// when nobody uses it that long.
	RefreshTokenLifetime = ConnectionIdle
)

// IsIdle reports whether nobody used the connection for ConnectionIdle.
func (c Connection) IsIdle(now time.Time) bool {
	last := c.CreatedAt
	if c.LastUsedAt != nil && c.LastUsedAt.After(last) {
		last = *c.LastUsedAt
	}
	return now.Sub(last) >= ConnectionIdle
}

const (
	// ConnectionTokenAccess is sent with every request, for an hour.
	ConnectionTokenAccess = "access"
	// ConnectionTokenRefresh gets a new access token, once.
	ConnectionTokenRefresh = "refresh"
)

// ConnectionToken is an access or refresh token of a connection. Only its
// SHA-256 hash is stored.
type ConnectionToken struct {
	ID           string    `db:"id"`
	ConnectionID string    `db:"connection_id"`
	Kind         string    `db:"kind"`
	TokenHash    string    `db:"token_hash"`
	ExpiresAt    time.Time `db:"expires_at"`
	// UsedAt is when a refresh token was exchanged. Showing it again means
	// someone else has a copy: the connection ends.
	UsedAt    *time.Time `db:"used_at"`
	CreatedAt time.Time  `db:"created_at"`
}

// CIToken is a token made by hand in Settings, for CI and scripts. It acts
// for its maker in one workspace with its rights, optionally for one app
// only. The raw token is shown once; only its hash is stored. Prefix is the
// start of the token after "pat_", so people tell their tokens apart.
type CIToken struct {
	ID          string `db:"id"`
	UserID      string `db:"user_id"`
	WorkspaceID string `db:"workspace_id"`
	Name        string `db:"name"`
	TokenHash   string `db:"token_hash"`
	Prefix      string `db:"prefix"`
	Rights      Rights `db:"rights"`
	// AppID limits the token to one app; nil reaches every app.
	AppID      *string    `db:"app_id"`
	LastUsedAt *time.Time `db:"last_used_at"`
	ExpiresAt  *time.Time `db:"expires_at"`
	CreatedAt  time.Time  `db:"created_at"`
	// AppName and MakerName are read with the token, for the lists.
	AppName   *string `db:"app_name"`
	MakerName string  `db:"maker_name"`
}

// IsExpired reports whether the token has an expiry that has passed.
func (t CIToken) IsExpired(now time.Time) bool {
	return t.ExpiresAt != nil && !t.ExpiresAt.After(now)
}

// Bearer is who a request with "Authorization: Bearer" acts for and what it
// may do: a connection or a CI token. Requests from a browser have none.
type Bearer struct {
	UserID      string
	WorkspaceID string
	Rights      Rights
	// Name is the connection's client name or the CI token's name, for the
	// activity.
	Name string
	// ConnectionID is set for a connection, CITokenID for a CI token.
	ConnectionID string
	CITokenID    string
	// AppID limits a CI token to one app.
	AppID string
	// Resource is the one place a connection's tokens work, empty for all.
	Resource  string
	ExpiresAt *time.Time
}

// Kind is "connection" or "CI token", for messages.
func (b Bearer) Kind() string {
	if b.CITokenID != "" {
		return "CI token"
	}
	return "connection"
}
