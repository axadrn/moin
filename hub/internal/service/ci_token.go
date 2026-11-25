package service

import (
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/google/uuid"
)

const (
	// CITokenMarker starts every CI token.
	CITokenMarker = "pat_"
	// MaxCITokensPerUser caps the CI tokens one account may hold.
	MaxCITokensPerUser = 50
	// MaxCITokenExpiryDays is the longest expiry a token can be given.
	MaxCITokenExpiryDays = 365
	maxCITokenNameLength = 64
	ciTokenPrefixLength  = 8
)

var (
	ErrCITokenName   = errors.New("ci token name must contain between 1 and 64 characters")
	ErrCITokenExpiry = errors.New("ci token expiry must be between 1 and 365 days")
	ErrCITokenLimit  = errors.New("ci token limit reached")
	// ErrCITokenMaker refuses a change to another person's CI token.
	ErrCITokenMaker = errors.New("only the person who made a ci token changes it")
)

// NewCIToken describes a CI token.
type NewCIToken struct {
	Name   string
	Rights model.Rights
	// App limits the token to one app, by name or ID; empty reaches every
	// app.
	App string
	// ExpiresInDays is between 1 and MaxCITokenExpiryDays, or 0 for a token
	// that never expires.
	ExpiresInDays int
}

// CITokenService makes, lists, edits and removes the CI tokens of a
// workspace, and authenticates them. A CI token acts for its maker with its
// rights, maybe for one app only.
type CITokenService struct {
	tokens     *repository.CITokens
	apps       *repository.Apps
	workspaces *WorkspaceService
	now        func() time.Time
}

func NewCITokenService(tokens *repository.CITokens, apps *repository.Apps, workspaces *WorkspaceService) *CITokenService {
	return &CITokenService{tokens: tokens, apps: apps, workspaces: workspaces, now: time.Now}
}

// Create stores a new token in the actor's workspace and returns it with
// the raw token, which is never available again.
func (s *CITokenService) Create(actor Actor, input NewCIToken) (*model.CIToken, string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxCITokenNameLength || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return nil, "", ErrCITokenName
	}
	if input.Rights&^allRights() != 0 {
		return nil, "", ErrRightsUnknown
	}
	if input.ExpiresInDays < 0 || input.ExpiresInDays > MaxCITokenExpiryDays {
		return nil, "", ErrCITokenExpiry
	}
	if err := requireMembership(s.workspaces, actor.UserID, actor.WorkspaceID); err != nil {
		return nil, "", err
	}
	now := s.now().UTC()
	token := &model.CIToken{
		ID:          uuid.New().String(),
		UserID:      actor.UserID,
		WorkspaceID: actor.WorkspaceID,
		Name:        name,
		Rights:      input.Rights | model.RightRead,
		CreatedAt:   now,
	}
	if app := strings.TrimSpace(input.App); app != "" {
		found, err := s.apps.ByIDOrNameForWorkspace(app, actor.WorkspaceID)
		if err != nil {
			return nil, "", err
		}
		token.AppID, token.AppName = &found.ID, &found.Name
	}
	if input.ExpiresInDays > 0 {
		expiresAt := now.Add(time.Duration(input.ExpiresInDays) * 24 * time.Hour)
		token.ExpiresAt = &expiresAt
	}
	raw, body, err := newToken(CITokenMarker)
	if err != nil {
		return nil, "", err
	}
	token.TokenHash, token.Prefix = hashToken(raw), body[:ciTokenPrefixLength]
	err = s.tokens.Create(token, MaxCITokensPerUser)
	if errors.Is(err, repository.ErrCITokenLimit) {
		return nil, "", ErrCITokenLimit
	}
	if err != nil {
		return nil, "", err
	}
	expires := "never"
	if token.ExpiresAt != nil {
		expires = token.ExpiresAt.Format(time.RFC3339)
	}
	details := map[string]string{"rights": token.Rights.String(), "expires": expires, "prefix": token.Prefix}
	if token.AppName != nil {
		details["app"] = *token.AppName
	}
	s.record(actor, "ci_token.created", *token, details)
	// Read back with its maker's name, as the lists show it.
	stored, err := s.tokens.ByID(token.WorkspaceID, token.ID)
	if err != nil {
		return nil, "", err
	}
	return stored, raw, nil
}

// List returns the tokens of the workspace the member sees, newest first:
// their own, and an owner every token of the workspace.
func (s *CITokenService) List(member *model.WorkspaceMember) ([]model.CIToken, error) {
	if member.Role == model.WorkspaceRoleOwner {
		return s.tokens.ListByWorkspace(member.WorkspaceID, "")
	}
	return s.tokens.ListByWorkspace(member.WorkspaceID, member.UserID)
}

// SetRights changes what a token may do. Only its maker changes it.
// presence is what PasskeyService.Present answered for the browser: a
// change that adds a right fails with it.
func (s *CITokenService) SetRights(actor Actor, id string, rights model.Rights, presence error) (*model.CIToken, error) {
	if rights&^allRights() != 0 {
		return nil, ErrRightsUnknown
	}
	token, err := s.tokens.ByID(actor.WorkspaceID, normalID(id))
	if err != nil {
		return nil, err
	}
	if token.UserID != actor.UserID {
		return nil, ErrCITokenMaker
	}
	rights |= model.RightRead
	if rights&^token.Rights != 0 && presence != nil {
		return nil, presence
	}
	if err := s.tokens.SetRights(token.ID, rights); err != nil {
		return nil, err
	}
	before := token.Rights
	token.Rights = rights
	if before != rights {
		s.record(actor, "ci_token.rights_changed", *token, map[string]string{"from": before.String(), "to": rights.String()})
	}
	return token, nil
}

// SetRight turns one right of a token on or off and leaves the others as
// they are now. Only its maker changes it.
func (s *CITokenService) SetRight(actor Actor, id string, right model.Rights, on bool) (*model.CIToken, error) {
	if right == model.RightRead {
		return nil, ErrRightsUnknown
	}
	for range swapTries {
		token, err := s.tokens.ByID(actor.WorkspaceID, normalID(id))
		if err != nil {
			return nil, err
		}
		if token.UserID != actor.UserID {
			return nil, ErrCITokenMaker
		}
		before := token.Rights
		after := withRight(before, right, on)
		if after == before {
			return token, nil
		}
		swapped, err := s.tokens.SwapRights(token.ID, before, after)
		if err != nil {
			return nil, err
		}
		if swapped {
			token.Rights = after
			s.record(actor, "ci_token.rights_changed", *token, map[string]string{"from": before.String(), "to": after.String()})
			return token, nil
		}
	}
	return nil, ErrRightsBusy
}

// Remove deletes a token: one's own, or as the workspace's owner any.
// Requests with it stop at once.
func (s *CITokenService) Remove(member *model.WorkspaceMember, id string, source Source) error {
	token, err := s.tokens.ByID(member.WorkspaceID, normalID(id))
	if err != nil {
		return err
	}
	if token.UserID != member.UserID && member.Role != model.WorkspaceRoleOwner {
		return repository.ErrCITokenNotFound
	}
	if err := s.tokens.Delete(token.ID); err != nil {
		return err
	}
	s.record(Actor{WorkspaceID: member.WorkspaceID, UserID: member.UserID, Source: source}, "ci_token.removed", *token, map[string]string{"prefix": token.Prefix})
	return nil
}

// DeleteAllForUser removes every token of a person. RevokeAllAccess, the
// admin kill switch, calls it.
func (s *CITokenService) DeleteAllForUser(userID string) error {
	tokens, err := s.tokens.ListByUser(userID)
	if err != nil {
		return err
	}
	if err := s.tokens.DeleteByUser(userID); err != nil {
		return err
	}
	for _, token := range tokens {
		s.record(Actor{WorkspaceID: token.WorkspaceID, UserID: userID, Source: Source{Via: ViaSystem}}, "ci_token.removed", token, map[string]string{"prefix": token.Prefix, "reason": "every token of the account was removed"})
	}
	return nil
}

// Authenticate returns who a raw "pat_" token acts for. Unknown and expired
// tokens are not found.
func (s *CITokenService) Authenticate(raw string) (*model.Bearer, error) {
	if !strings.HasPrefix(raw, CITokenMarker) || len(raw) == len(CITokenMarker) {
		return nil, repository.ErrCITokenNotFound
	}
	token, err := s.tokens.ByTokenHash(hashToken(raw))
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if token.IsExpired(now) {
		return nil, repository.ErrCITokenNotFound
	}
	if token.LastUsedAt == nil || now.Sub(*token.LastUsedAt) >= touchInterval {
		if err := s.tokens.TouchLastUsed(token.ID, now); err != nil {
			slog.Warn("failed to record a ci token's use", "error", err, "token_id", token.ID)
		}
	}
	bearer := &model.Bearer{
		UserID:      token.UserID,
		WorkspaceID: token.WorkspaceID,
		Rights:      token.Rights,
		Name:        token.Name,
		CITokenID:   token.ID,
		ExpiresAt:   token.ExpiresAt,
	}
	if token.AppID != nil {
		bearer.AppID = *token.AppID
	}
	return bearer, nil
}

// record writes a token change to the activity of the token's workspace,
// with the token's name, never the token.
func (s *CITokenService) record(actor Actor, action string, token model.CIToken, details map[string]string) {
	if s.workspaces == nil {
		return
	}
	details["name"] = token.Name
	s.workspaces.record(actor, action, "ci_token", token.Name, details)
}
