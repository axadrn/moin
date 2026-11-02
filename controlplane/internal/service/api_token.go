package service

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/google/uuid"
)

const (
	// APITokenMarker starts every raw personal API token.
	APITokenMarker = "pat_"
	// MaxAPITokensPerUser caps the tokens one account may hold.
	MaxAPITokensPerUser = 50
	// MaxAPITokenExpiryDays is the longest expiry a token can be given.
	MaxAPITokenExpiryDays = 365
	maxAPITokenNameLength = 64
	apiTokenPrefixLength  = 8
	// apiTokenTouchInterval limits last_used_at writes to one per minute.
	apiTokenTouchInterval = time.Minute
)

var (
	ErrAPITokenName      = errors.New("api token name must contain between 1 and 64 characters")
	ErrAPITokenScope     = errors.New("api token scopes must be read or read and write")
	ErrAPITokenExpiry    = errors.New("api token expiry must be between 1 and 365 days")
	ErrAPITokenLimit     = errors.New("api token limit reached")
	ErrAPITokenWorkspace = errors.New("api token workspace must be one of your workspaces")
)

var apiTokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// CreateAPITokenInput describes a new personal API token.
type CreateAPITokenInput struct {
	Name string
	// Scopes holds "read", "write" or both. Write implies read. Empty means
	// read only.
	Scopes []string
	// ExpiresInDays is between 1 and MaxAPITokenExpiryDays, or 0 for a token
	// that never expires.
	ExpiresInDays int
	// WorkspaceID binds the token to one of the user's workspaces.
	WorkspaceID string
	// Source is how the request came in, for the activity of the workspace.
	Source Source
}

// APITokenService creates, lists, revokes and authenticates personal API
// tokens. A token acts for its user with the scopes it was given.
type APITokenService struct {
	tokens     repository.APITokenRepository
	workspaces *WorkspaceService
	now        func() time.Time
}

func NewAPITokenService(tokens repository.APITokenRepository, workspaces *WorkspaceService) *APITokenService {
	return &APITokenService{tokens: tokens, workspaces: workspaces, now: time.Now}
}

// Create stores a new token and returns it with the raw token, which is never
// available again.
func (s *APITokenService) Create(userID string, input CreateAPITokenInput) (*model.APIToken, string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxAPITokenNameLength || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return nil, "", ErrAPITokenName
	}
	scopes, err := normalizeAPITokenScopes(input.Scopes)
	if err != nil {
		return nil, "", err
	}
	if input.ExpiresInDays < 0 || input.ExpiresInDays > MaxAPITokenExpiryDays {
		return nil, "", ErrAPITokenExpiry
	}
	err = s.requireMembership(userID, input.WorkspaceID)
	if err != nil {
		return nil, "", err
	}
	count, err := s.tokens.CountByUserID(userID)
	if err != nil {
		return nil, "", err
	}
	if count >= MaxAPITokensPerUser {
		return nil, "", ErrAPITokenLimit
	}

	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	if err != nil {
		return nil, "", fmt.Errorf("generate api token: %w", err)
	}
	body := strings.ToLower(apiTokenEncoding.EncodeToString(secret))
	raw := APITokenMarker + body
	now := s.now().UTC()
	token := &model.APIToken{
		ID:          uuid.New().String(),
		UserID:      userID,
		WorkspaceID: input.WorkspaceID,
		Name:        name,
		TokenHash:   hashToken(raw),
		Prefix:      body[:apiTokenPrefixLength],
		Scopes:      scopes,
		CreatedAt:   now,
	}
	if input.ExpiresInDays > 0 {
		expiresAt := now.Add(time.Duration(input.ExpiresInDays) * 24 * time.Hour)
		token.ExpiresAt = &expiresAt
	}
	err = s.tokens.Create(token)
	if err != nil {
		return nil, "", err
	}
	expires := "never"
	if token.ExpiresAt != nil {
		expires = token.ExpiresAt.Format(time.RFC3339)
	}
	s.record(userID, input.Source, "api_token.created", *token, map[string]string{"scopes": token.Scopes, "expires": expires, "prefix": token.Prefix})
	return token, raw, nil
}

// List returns the tokens of a user, newest first. Expired tokens stay listed
// until they are revoked.
func (s *APITokenService) List(userID string) ([]model.APIToken, error) {
	return s.tokens.ListByUserID(userID)
}

// Revoke deletes one token of the user. Another user's token is not found.
func (s *APITokenService) Revoke(userID, tokenID string, source Source) error {
	tokens, err := s.tokens.ListByUserID(userID)
	if err != nil {
		return err
	}
	err = s.tokens.DeleteByIDForUser(tokenID, userID)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if token.ID == tokenID {
			s.record(userID, source, "api_token.revoked", token, map[string]string{"prefix": token.Prefix})
		}
	}
	return nil
}

// RevokeAllForUser deletes every token of the user. Account recovery, password
// changes and the admin kill switch call it through AuthService.
func (s *APITokenService) RevokeAllForUser(userID string) error {
	tokens, err := s.tokens.ListByUserID(userID)
	if err != nil {
		return err
	}
	err = s.tokens.DeleteByUserID(userID)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		s.record(userID, Source{Via: ViaSystem}, "api_token.revoked", token, map[string]string{"prefix": token.Prefix, "reason": "every token of the account was revoked"})
	}
	return nil
}

// record writes a token change to the activity of the token's workspace,
// with the token's name, never the token.
func (s *APITokenService) record(userID string, source Source, action string, token model.APIToken, details map[string]string) {
	if s.workspaces == nil {
		return
	}
	details["name"] = token.Name
	s.workspaces.record(Actor{WorkspaceID: token.WorkspaceID, UserID: userID, Source: source}, action, "api_token", token.Name, details)
}

// Authenticate returns the token for a raw "pat_" token. Unknown and expired
// tokens are not found.
func (s *APITokenService) Authenticate(raw string) (*model.APIToken, error) {
	if !strings.HasPrefix(raw, APITokenMarker) || len(raw) == len(APITokenMarker) {
		return nil, repository.ErrAPITokenNotFound
	}
	token, err := s.tokens.ByTokenHash(hashToken(raw))
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if token.IsExpired(now) {
		return nil, repository.ErrAPITokenNotFound
	}
	s.TouchLastUsed(token, now)
	return token, nil
}

// TouchLastUsed records a use of the token, at most once per minute so busy
// clients do not write on every request.
func (s *APITokenService) TouchLastUsed(token *model.APIToken, now time.Time) {
	if token.LastUsedAt != nil && now.Sub(*token.LastUsedAt) < apiTokenTouchInterval {
		return
	}
	err := s.tokens.TouchLastUsed(token.ID, now)
	if err != nil {
		slog.Warn("failed to record api token use", "error", err, "token_id", token.ID)
		return
	}
	token.LastUsedAt = &now
}

func (s *APITokenService) requireMembership(userID, workspaceID string) error {
	if workspaceID == "" {
		return ErrAPITokenWorkspace
	}
	memberships, err := s.workspaces.ListForUser(userID)
	if err != nil {
		return err
	}
	for _, membership := range memberships {
		if membership.ID == workspaceID {
			return nil
		}
	}
	return ErrAPITokenWorkspace
}

// normalizeAPITokenScopes turns requested scopes into the stored form. Write
// implies read.
func normalizeAPITokenScopes(requested []string) (string, error) {
	write := false
	for _, scope := range requested {
		switch strings.TrimSpace(scope) {
		case model.APITokenScopeRead:
		case model.APITokenScopeWrite:
			write = true
		default:
			return "", ErrAPITokenScope
		}
	}
	if write {
		return model.APITokenScopeRead + "," + model.APITokenScopeWrite, nil
	}
	return model.APITokenScopeRead, nil
}
