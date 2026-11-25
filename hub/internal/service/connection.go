package service

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/internal/link"
	"github.com/google/uuid"
)

const (
	// AccessTokenMarker starts every access token of a connection.
	AccessTokenMarker = "at_"
	// RefreshTokenMarker starts every refresh token of a connection.
	RefreshTokenMarker = "rt_"
	// MaxConnectionsPerUser caps the live connections of one account.
	MaxConnectionsPerUser = 50
	maxClientNameRunes    = 64
	// touchInterval limits last_used_at writes to one per minute.
	touchInterval = time.Minute
)

var (
	// ErrConnectionWorkspace is a workspace the person is not in.
	ErrConnectionWorkspace = errors.New("the workspace must be one of yours")
	// ErrConnectionLimit is an account with MaxConnectionsPerUser live
	// connections.
	ErrConnectionLimit = errors.New("connection limit reached")
	// ErrRightsUnknown is a right that does not exist.
	ErrRightsUnknown = errors.New("rights must be read, deploy, env, delete or servers")
	// ErrRefreshInvalid is a refresh token that is unknown, expired, of
	// another client, or of a connection that ended.
	ErrRefreshInvalid = errors.New("the refresh token is invalid")
	// ErrRefreshReused is a refresh token shown a second time: someone else
	// has a copy, so the whole connection ended.
	ErrRefreshReused = errors.New("the refresh token was used before; the connection ended")
)

// tokenEncoding writes the random part of every token.
var tokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newToken makes a raw token: marker and 32 random bytes in lower case
// base32. It returns the raw token and its body without the marker.
func newToken(marker string) (string, string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	body := strings.ToLower(tokenEncoding.EncodeToString(secret))
	return marker + body, body, nil
}

// Tokens are what a client gets when it is let in or refreshes: an access
// token for an hour and a refresh token for the next pair.
type Tokens struct {
	Access     string
	Refresh    string
	ExpiresIn  time.Duration
	Connection *model.Connection
}

// ConnectionService runs the connections: a person lets a client in from
// the browser with some rights, the client gets tokens and refreshes them,
// and the person edits or removes the connection at any time.
type ConnectionService struct {
	connections *repository.Connections
	workspaces  *WorkspaceService
	now         func() time.Time
}

func NewConnectionService(connections *repository.Connections, workspaces *WorkspaceService) *ConnectionService {
	return &ConnectionService{connections: connections, workspaces: workspaces, now: time.Now}
}

// NewConnection is what a person approved: a client, a workspace of theirs
// and the rights. ClientID and Resource are for AI agents (OAuth).
type NewConnection struct {
	UserID      string
	WorkspaceID string
	ClientName  string
	Rights      model.Rights
	ClientID    string
	Resource    string
	Source      Source
}

// Create stores the connection a person approved. The approval itself, the
// passkey or a fresh sign-in, is checked before by the caller.
func (s *ConnectionService) Create(input NewConnection) (*model.Connection, error) {
	input.WorkspaceID = normalID(input.WorkspaceID)
	if err := requireMembership(s.workspaces, input.UserID, input.WorkspaceID); err != nil {
		return nil, err
	}
	if input.Rights&^allRights() != 0 {
		return nil, ErrRightsUnknown
	}
	connection := &model.Connection{
		ID:          uuid.New().String(),
		UserID:      input.UserID,
		WorkspaceID: input.WorkspaceID,
		ClientName:  CleanClientName(input.ClientName),
		Rights:      input.Rights | model.RightRead,
		ClientID:    input.ClientID,
		Resource:    input.Resource,
		CreatedAt:   s.now().UTC(),
	}
	err := s.connections.Create(connection, MaxConnectionsPerUser)
	if errors.Is(err, repository.ErrConnectionLimit) {
		return nil, ErrConnectionLimit
	}
	if err != nil {
		return nil, err
	}
	s.record(*connection, input.Source, "connection.approved", map[string]string{"rights": connection.Rights.String()})
	return connection, nil
}

// Issue gives a connection a new access and refresh token.
func (s *ConnectionService) Issue(connection *model.Connection) (Tokens, error) {
	now := s.now().UTC()
	access, _, err := newToken(AccessTokenMarker)
	if err != nil {
		return Tokens{}, err
	}
	refresh, _, err := newToken(RefreshTokenMarker)
	if err != nil {
		return Tokens{}, err
	}
	for _, token := range []model.ConnectionToken{
		{ID: uuid.New().String(), ConnectionID: connection.ID, Kind: model.ConnectionTokenAccess, TokenHash: hashToken(access), ExpiresAt: now.Add(model.AccessTokenLifetime), CreatedAt: now},
		{ID: uuid.New().String(), ConnectionID: connection.ID, Kind: model.ConnectionTokenRefresh, TokenHash: hashToken(refresh), ExpiresAt: now.Add(model.RefreshTokenLifetime), CreatedAt: now},
	} {
		if err := s.connections.AddToken(token); err != nil {
			return Tokens{}, err
		}
	}
	if err := s.connections.Touch(connection.ID, now); err != nil {
		return Tokens{}, err
	}
	return Tokens{Access: access, Refresh: refresh, ExpiresIn: model.AccessTokenLifetime, Connection: connection}, nil
}

// Refresh exchanges a refresh token for a new pair, once. A refresh token
// shown a second time ends the whole connection: one copy is in the wrong
// hands, and nobody can tell which. clientID must be the connection's
// client, empty for the device flow.
func (s *ConnectionService) Refresh(raw, clientID string) (Tokens, error) {
	if !strings.HasPrefix(raw, RefreshTokenMarker) {
		return Tokens{}, ErrRefreshInvalid
	}
	token, connection, err := s.connections.Token(hashToken(raw), model.ConnectionTokenRefresh)
	if errors.Is(err, repository.ErrConnectionTokenNotFound) {
		return Tokens{}, ErrRefreshInvalid
	}
	if err != nil {
		return Tokens{}, err
	}
	now := s.now().UTC()
	if connection.ClientID != clientID {
		return Tokens{}, ErrRefreshInvalid
	}
	if token.UsedAt != nil {
		s.end(*connection, Source{Via: ViaSystem}, "the refresh token was used twice")
		return Tokens{}, ErrRefreshReused
	}
	if connection.IsIdle(now) {
		s.end(*connection, Source{Via: ViaSystem}, "unused for 90 days")
		return Tokens{}, ErrRefreshInvalid
	}
	if !token.ExpiresAt.After(now) {
		return Tokens{}, ErrRefreshInvalid
	}
	err = s.connections.UseRefresh(token.ID, now)
	if errors.Is(err, repository.ErrRefreshTokenUsed) {
		s.end(*connection, Source{Via: ViaSystem}, "the refresh token was used twice")
		return Tokens{}, ErrRefreshReused
	}
	if err != nil {
		return Tokens{}, err
	}
	if err := s.connections.DeleteExpiredTokens(connection.ID, now); err != nil {
		slog.Warn("failed to delete expired tokens", "error", err, "connection_id", connection.ID)
	}
	return s.Issue(connection)
}

// Authenticate returns who an access token acts for. Unknown and expired
// tokens, and those of an ended or idle connection, are not found. Rights
// come from the connection as it is now, so an edit counts at once.
func (s *ConnectionService) Authenticate(raw string) (*model.Bearer, error) {
	if !strings.HasPrefix(raw, AccessTokenMarker) {
		return nil, repository.ErrConnectionTokenNotFound
	}
	token, connection, err := s.connections.Token(hashToken(raw), model.ConnectionTokenAccess)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if !token.ExpiresAt.After(now) {
		return nil, repository.ErrConnectionTokenNotFound
	}
	if connection.IsIdle(now) {
		s.end(*connection, Source{Via: ViaSystem}, "unused for 90 days")
		return nil, repository.ErrConnectionTokenNotFound
	}
	if connection.LastUsedAt == nil || now.Sub(*connection.LastUsedAt) >= touchInterval {
		if err := s.connections.Touch(connection.ID, now); err != nil {
			slog.Warn("failed to record a connection's use", "error", err, "connection_id", connection.ID)
		}
	}
	return &model.Bearer{
		UserID:       connection.UserID,
		WorkspaceID:  connection.WorkspaceID,
		Rights:       connection.Rights,
		Name:         connection.ClientName,
		ConnectionID: connection.ID,
		Resource:     connection.Resource,
		ExpiresAt:    &token.ExpiresAt,
	}, nil
}

// Revoke ends the connection of an access or refresh token, as RFC 7009
// asks: an unknown token is no error, there is nothing left to end.
func (s *ConnectionService) Revoke(raw string, source Source) error {
	kind := model.ConnectionTokenAccess
	if strings.HasPrefix(raw, RefreshTokenMarker) {
		kind = model.ConnectionTokenRefresh
	}
	_, connection, err := s.connections.Token(hashToken(raw), kind)
	if errors.Is(err, repository.ErrConnectionTokenNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s.end(*connection, source, "logged out")
	return nil
}

// List returns the live connections of a person, newest first.
func (s *ConnectionService) List(userID string) ([]model.Connection, error) {
	return s.connections.ListByUser(userID)
}

// SetRights changes what a connection of the person may do. The next
// request of the connection has the new rights.
func (s *ConnectionService) SetRights(userID, id string, rights model.Rights, source Source) (*model.Connection, error) {
	connection, err := s.own(userID, id)
	if err != nil {
		return nil, err
	}
	rights |= model.RightRead
	if err := s.connections.SetRights(connection.ID, userID, rights); err != nil {
		return nil, err
	}
	before := connection.Rights
	connection.Rights = rights
	if before != rights {
		s.record(*connection, source, "connection.rights_changed", map[string]string{"from": before.String(), "to": rights.String()})
	}
	return connection, nil
}

// Remove ends a connection of the person and every token of it, at once.
func (s *ConnectionService) Remove(userID, id string, source Source) error {
	connection, err := s.own(userID, id)
	if err != nil {
		return err
	}
	return s.endWith(*connection, source, "removed")
}

// EndAllForUser ends every connection of a person: the admin kill switch,
// a recovery key and a new email call it.
func (s *ConnectionService) EndAllForUser(userID string) error {
	connections, err := s.connections.ListByUser(userID)
	if err != nil {
		return err
	}
	for _, connection := range connections {
		if err := s.endWith(connection, Source{Via: ViaSystem}, "every connection of the account ended"); err != nil && !errors.Is(err, repository.ErrConnectionNotFound) {
			return err
		}
	}
	return nil
}

// own is a live connection of the person; another person's is not found.
func (s *ConnectionService) own(userID, id string) (*model.Connection, error) {
	connection, err := s.connections.ByID(normalID(id))
	if err != nil {
		return nil, err
	}
	if connection.UserID != userID {
		return nil, repository.ErrConnectionNotFound
	}
	return connection, nil
}

// end ends a connection and logs a failure: the callers answer the client
// either way.
func (s *ConnectionService) end(connection model.Connection, source Source, reason string) {
	if err := s.endWith(connection, source, reason); err != nil && !errors.Is(err, repository.ErrConnectionNotFound) {
		slog.Error("failed to end a connection", "error", err, "connection_id", connection.ID)
	}
}

func (s *ConnectionService) endWith(connection model.Connection, source Source, reason string) error {
	if err := s.connections.End(connection.ID, s.now().UTC()); err != nil {
		return err
	}
	s.record(connection, source, "connection.ended", map[string]string{"reason": reason})
	return nil
}

// record writes a change of a connection to the activity of its
// workspace, with the client's name, never a token.
func (s *ConnectionService) record(connection model.Connection, source Source, action string, details map[string]string) {
	if s.workspaces == nil {
		return
	}
	details["name"] = connection.ClientName
	s.workspaces.record(Actor{WorkspaceID: connection.WorkspaceID, UserID: connection.UserID, Source: source}, action, "connection", connection.ClientName, details)
}

// CleanClientName keeps a client's name printable and short. A client
// without a name is "A client".
func CleanClientName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if runes := []rune(name); len(runes) > maxClientNameRunes {
		name = strings.TrimSpace(string(runes[:maxClientNameRunes]))
	}
	if name == "" {
		return "A client"
	}
	return name
}

// requireMembership refuses a workspace the user is not in.
func requireMembership(workspaces *WorkspaceService, userID, workspaceID string) error {
	if workspaceID == "" {
		return ErrConnectionWorkspace
	}
	memberships, err := workspaces.ListForUser(userID)
	if err != nil {
		return err
	}
	for _, membership := range memberships {
		if membership.ID == workspaceID {
			return nil
		}
	}
	return ErrConnectionWorkspace
}

// normalID is the UUID with dashes the database keeps, for an ID as the
// API shows it, without dashes.
func normalID(id string) string {
	if parsed, ok := link.Parse(id); ok {
		return parsed
	}
	return id
}

func allRights() model.Rights {
	var all model.Rights
	for _, right := range model.AllRights {
		all |= right
	}
	return all
}
