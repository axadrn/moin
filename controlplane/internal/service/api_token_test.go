package service

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

func TestRevokeAccessRevokesAPITokens(t *testing.T) {
	for _, test := range []struct {
		name   string
		revoke func(*AuthService) error
		kept   []string
	}{
		{name: "all", revoke: func(auth *AuthService) error { return auth.RevokeAllAccess("user") }},
		{name: "others", revoke: func(auth *AuthService) error { return auth.RevokeOtherAccess("user", "current") }, kept: []string{"current"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := testutil.NewDatabase(t)
			user := testutil.SeedUser(t, database, testutil.UserOptions{ID: "user"})
			other := testutil.SeedUser(t, database, testutil.UserOptions{ID: "other"})
			tokens := repository.NewAPITokens(database)
			sessions := repository.NewSessions(database)
			now := time.Now()
			for _, token := range []model.APIToken{
				{ID: "user-1", UserID: "user", WorkspaceID: user.Workspace.ID},
				{ID: "user-2", UserID: "user", WorkspaceID: user.Workspace.ID},
				{ID: "other-1", UserID: "other", WorkspaceID: other.Workspace.ID},
			} {
				token.TokenHash, token.Prefix, token.Scopes, token.Kind, token.CreatedAt = token.ID, token.ID, "read", model.APITokenKindPersonal, now
				if err := tokens.Create(&token); err != nil {
					t.Fatal(err)
				}
			}
			for _, session := range []model.Session{
				{ID: "current", UserID: "user"},
				{ID: "second", UserID: "user"},
			} {
				session.TokenHash, session.ExpiresAt, session.LastSeenAt, session.CreatedAt = session.ID, now.Add(time.Hour), now, now
				if err := sessions.Create(&session); err != nil {
					t.Fatal(err)
				}
			}
			auth := &AuthService{sessionRepository: sessions}

			err := test.revoke(auth)
			if !errors.Is(err, errAPITokensNotConfigured) {
				t.Fatalf("revoke without api tokens error = %v, want %v", err, errAPITokensNotConfigured)
			}

			auth.UseAPITokens(&APITokenService{tokens: tokens, now: time.Now})
			err = test.revoke(auth)
			if err != nil {
				t.Fatal(err)
			}
			left, err := sessions.ListByUserID("user", now)
			if err != nil {
				t.Fatal(err)
			}
			var kept []string
			for _, session := range left {
				kept = append(kept, session.ID)
			}
			if !slices.Equal(kept, test.kept) {
				t.Fatalf("sessions kept = %v, want %v", kept, test.kept)
			}
			for _, user := range []struct {
				id    string
				count int
			}{{"user", 0}, {"other", 1}} {
				count, err := tokens.CountByUserID(user.id)
				if err != nil || count != user.count {
					t.Fatalf("tokens left for %s = %d, %v, want %d", user.id, count, err, user.count)
				}
			}
		})
	}
}

func TestAPITokenAuthenticateRecordsUseAtMostOncePerMinute(t *testing.T) {
	database := testutil.NewDatabase(t)
	fixture := testutil.SeedUser(t, database, testutil.UserOptions{ID: "user"})
	store := repository.NewAPITokens(database)
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	now := start
	service := &APITokenService{tokens: store, now: func() time.Time { return now }}
	expiresAt := now.Add(2 * time.Minute)
	raw := APITokenMarker + "abcdefghijklmnopqrstuvwxyz234567abcdefghijklmnopqrst"
	err := store.Create(&model.APIToken{ID: "token", UserID: "user", WorkspaceID: fixture.Workspace.ID, TokenHash: hashToken(raw), Prefix: "abcdefgh", Scopes: "read", Kind: model.APITokenKindPersonal, ExpiresAt: &expiresAt, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}

	for _, step := range []struct {
		advance  time.Duration
		lastUsed time.Time
	}{
		{advance: 0, lastUsed: start},
		{advance: 30 * time.Second, lastUsed: start},
		{advance: 30 * time.Second, lastUsed: start.Add(time.Minute)},
		{advance: 59 * time.Second, lastUsed: start.Add(time.Minute)},
	} {
		now = now.Add(step.advance)
		token, err := service.Authenticate(raw)
		if err != nil {
			t.Fatalf("Authenticate() at %s error = %v", now, err)
		}
		stored, err := store.ByTokenHash(hashToken(raw))
		if err != nil {
			t.Fatal(err)
		}
		if token.UserID != "user" || stored.LastUsedAt == nil || !stored.LastUsedAt.Equal(step.lastUsed) {
			t.Fatalf("at %s: user %q, last used %v, want %s", now, token.UserID, stored.LastUsedAt, step.lastUsed)
		}
	}

	now = expiresAt
	_, err = service.Authenticate(raw)
	if !errors.Is(err, repository.ErrAPITokenNotFound) {
		t.Fatalf("expired token error = %v, want ErrAPITokenNotFound", err)
	}
	for _, bogus := range []string{"", "pat_", "session-token", APITokenMarker + "unknown"} {
		_, err := service.Authenticate(bogus)
		if err == nil {
			t.Fatalf("Authenticate(%q) succeeded", bogus)
		}
	}
}

func TestNormalizeAPITokenScopes(t *testing.T) {
	for _, test := range []struct {
		requested []string
		want      string
		fails     bool
	}{
		{requested: nil, want: "read"},
		{requested: []string{"read"}, want: "read"},
		{requested: []string{"write"}, want: "read,write"},
		{requested: []string{"write", "read", "read"}, want: "read,write"},
		{requested: []string{"read", "admin"}, fails: true},
		{requested: []string{""}, fails: true},
	} {
		got, err := normalizeAPITokenScopes(test.requested)
		if test.fails {
			if !errors.Is(err, ErrAPITokenScope) {
				t.Fatalf("%v: error = %v, want ErrAPITokenScope", test.requested, err)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf("%v = %q, %v, want %q", test.requested, got, err, test.want)
		}
	}
}
