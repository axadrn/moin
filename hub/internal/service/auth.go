package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/validation"
	"github.com/google/uuid"
)

var (
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrInvalidEmail       = errors.New("invalid email address")
	ErrNameRequired       = errors.New("name is required")
)

type AuthService struct {
	// beta closes sign-up to everyone it does not let in; nil keeps it open.
	beta                   *BetaService
	userRepository         *repository.Users
	tokenRepository        *repository.Tokens
	sessionRepository      *repository.Sessions
	workspaceService       *WorkspaceService
	emailService           *EmailService
	isProduction           bool
	sessionExpiry          time.Duration
	tokenEmailVerifyExpiry time.Duration
	tokenEmailChangeExpiry time.Duration
	sessionCleanupMu       sync.Mutex
	lastSessionCleanup     time.Time
	connections            *ConnectionService
	ciTokens               *CITokenService
	now                    func() time.Time
}

func NewAuthService(
	userRepository *repository.Users,
	tokenRepository *repository.Tokens,
	sessionRepository *repository.Sessions,
	workspaceService *WorkspaceService,
	emailService *EmailService,
	isProduction bool,
	sessionExpiry time.Duration,
	tokenEmailVerifyExpiry time.Duration,
	tokenEmailChangeExpiry time.Duration,
) *AuthService {
	return &AuthService{
		userRepository:         userRepository,
		tokenRepository:        tokenRepository,
		sessionRepository:      sessionRepository,
		workspaceService:       workspaceService,
		emailService:           emailService,
		isProduction:           isProduction,
		sessionExpiry:          sessionExpiry,
		tokenEmailVerifyExpiry: tokenEmailVerifyExpiry,
		tokenEmailChangeExpiry: tokenEmailChangeExpiry,
		now:                    time.Now,
	}
}

func (s *AuthService) GenerateToken() (string, error) {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// CreateSession stores a new session for userID and returns it with the raw
// token. The caller decides how the token reaches the client.
func (s *AuthService) CreateSession(r *http.Request, userID string) (*model.Session, string, error) {
	session, token, err := s.newSession(r, userID)
	if err != nil {
		return nil, "", err
	}
	err = s.sessionRepository.Create(session)
	if err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}
	return session, token, nil
}

func (s *AuthService) StartSession(w http.ResponseWriter, r *http.Request, userID string) (*model.Session, error) {
	session, token, err := s.CreateSession(r, userID)
	if err != nil {
		return nil, err
	}
	if cookie, err := r.Cookie("session_token"); err == nil && cookie.Value != "" {
		if err := s.sessionRepository.DeleteByTokenHash(hashToken(cookie.Value)); err != nil {
			_ = s.sessionRepository.DeleteByTokenHash(session.TokenHash)
			return nil, fmt.Errorf("replace existing session: %w", err)
		}
	}

	s.SetSessionCookie(w, token, session.ExpiresAt)
	return session, nil
}

func (s *AuthService) StartImpersonation(w http.ResponseWriter, r *http.Request, operatorUserID, targetUserID string) (*model.Session, error) {
	session, token, err := s.newSession(r, targetUserID)
	if err != nil {
		return nil, err
	}
	session.ImpersonatorUserID = &operatorUserID
	maximumExpiry := time.Now().Add(time.Hour)
	if session.ExpiresAt.After(maximumExpiry) {
		session.ExpiresAt = maximumExpiry
	}
	if err := s.sessionRepository.Create(session); err != nil {
		return nil, fmt.Errorf("create impersonation session: %w", err)
	}
	if cookie, err := r.Cookie("impersonation_token"); err == nil && cookie.Value != "" {
		if err := s.sessionRepository.DeleteByTokenHash(hashToken(cookie.Value)); err != nil {
			_ = s.sessionRepository.DeleteByTokenHash(session.TokenHash)
			return nil, fmt.Errorf("replace impersonation session: %w", err)
		}
	}

	s.setCookie(w, "impersonation_token", token, session.ExpiresAt)
	return session, nil
}

// CreateFreshSession stores a new session for userID, revokes every other
// session of the user, and returns the new session with the raw token.
func (s *AuthService) CreateFreshSession(r *http.Request, userID string) (*model.Session, string, error) {
	session, token, err := s.newSession(r, userID)
	if err != nil {
		return nil, "", err
	}
	err = s.sessionRepository.ReplaceAllForUser(session)
	if err != nil {
		return nil, "", fmt.Errorf("replace all sessions: %w", err)
	}
	return session, token, nil
}

func (s *AuthService) StartFreshSession(w http.ResponseWriter, r *http.Request, userID string) (*model.Session, error) {
	session, token, err := s.CreateFreshSession(r, userID)
	if err != nil {
		return nil, err
	}

	s.SetSessionCookie(w, token, session.ExpiresAt)
	return session, nil
}

func (s *AuthService) newSession(r *http.Request, userID string) (*model.Session, string, error) {
	token, err := s.GenerateToken()
	if err != nil {
		return nil, "", fmt.Errorf("generate session token: %w", err)
	}

	now := time.Now()
	session := &model.Session{
		ID:          uuid.New().String(),
		UserID:      userID,
		TokenHash:   hashToken(token),
		UserAgent:   truncateUserAgent(r.UserAgent()),
		ExpiresAt:   now.Add(s.sessionExpiry),
		LastSeenAt:  now,
		CreatedAt:   now,
		ConfirmedAt: now,
	}
	return session, token, nil
}

func (s *AuthService) AuthenticateSession(token string) (*model.Session, error) {
	if token == "" {
		return nil, repository.ErrSessionNotFound
	}

	session, err := s.sessionRepository.ByTokenHash(hashToken(token))
	if err != nil {
		return nil, err
	}

	now := time.Now()
	s.cleanupExpiredSessions(now)
	if session.IsExpired(now) {
		_ = s.sessionRepository.DeleteByTokenHash(session.TokenHash)
		return nil, repository.ErrSessionNotFound
	}

	if now.Sub(session.LastSeenAt) >= 5*time.Minute {
		if err := s.sessionRepository.UpdateLastSeen(session.ID, now); err != nil {
			slog.Warn("failed to update session activity", "error", err, "session_id", session.ID)
			return session, nil
		}
		session.LastSeenAt = now
	}

	return session, nil
}

func (s *AuthService) Sessions(userID string) ([]model.Session, error) {
	return s.sessionRepository.ListByUserID(userID, time.Now())
}

func (s *AuthService) RevokeSession(userID, sessionID string) error {
	return s.sessionRepository.DeleteByIDForUser(sessionID, userID)
}

func (s *AuthService) RevokeOtherSessions(userID, currentSessionID string) error {
	return s.sessionRepository.DeleteOthersForUser(userID, currentSessionID)
}

func (s *AuthService) RevokeAllSessions(userID string) error {
	return s.sessionRepository.DeleteAllForUser(userID)
}

func (s *AuthService) EndSession(r *http.Request) error {
	for _, name := range []string{"impersonation_token", "session_token"} {
		cookie, err := r.Cookie(name)
		if errors.Is(err, http.ErrNoCookie) {
			continue
		}
		if err != nil {
			return err
		}
		if err := s.sessionRepository.DeleteByTokenHash(hashToken(cookie.Value)); err != nil {
			return err
		}
	}
	return nil
}

func (s *AuthService) SetSessionCookie(w http.ResponseWriter, token string, expiry time.Time) {
	s.setCookie(w, "session_token", token, expiry)
}

func (s *AuthService) setCookie(w http.ResponseWriter, name, token string, expiry time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Expires:  expiry,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isProduction,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *AuthService) ClearSessionCookie(w http.ResponseWriter) {
	s.clearCookie(w, "impersonation_token")
	s.clearCookie(w, "session_token")
}

func (s *AuthService) ClearImpersonationCookie(w http.ResponseWriter) {
	s.clearCookie(w, "impersonation_token")
}

func (s *AuthService) ClearCookie(w http.ResponseWriter, name string) {
	s.clearCookie(w, name)
}

func (s *AuthService) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isProduction,
		SameSite: http.SameSiteLaxMode,
	})
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func truncateUserAgent(userAgent string) string {
	const maxLength = 512
	if len(userAgent) <= maxLength {
		return userAgent
	}
	return userAgent[:maxLength]
}

func (s *AuthService) cleanupExpiredSessions(now time.Time) {
	s.sessionCleanupMu.Lock()
	if !s.lastSessionCleanup.IsZero() && now.Sub(s.lastSessionCleanup) < time.Hour {
		s.sessionCleanupMu.Unlock()
		return
	}
	s.lastSessionCleanup = now
	s.sessionCleanupMu.Unlock()

	if err := s.sessionRepository.DeleteExpired(now); err != nil {
		slog.Warn("failed to clean up expired sessions", "error", err)
	}
	if s.tokenRepository != nil {
		if err := s.tokenRepository.DeleteExpired(now); err != nil {
			slog.Warn("failed to clean up expired tokens", "error", err)
		}
	}
}

func (s *AuthService) RequestEmailChange(userID, newEmail string) error {
	newEmail = strings.TrimSpace(strings.ToLower(newEmail))

	err := validation.ValidateEmail(newEmail)
	if err != nil {
		return ErrInvalidEmail
	}

	user, err := s.userRepository.ByID(userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return fmt.Errorf("user not found: %w", err)
		}
		return fmt.Errorf("failed to get user: %w", err)
	}

	if newEmail == user.Email {
		return fmt.Errorf("email is already set to this value: %w", ErrInvalidEmail)
	}

	existingUser, err := s.userRepository.ByEmail(newEmail)
	if err != nil && !errors.Is(err, repository.ErrUserNotFound) {
		return fmt.Errorf("failed to check email: %w", err)
	}
	if existingUser != nil {
		return fmt.Errorf("email already in use: %w", ErrEmailAlreadyExists)
	}

	err = s.tokenRepository.DeleteByUserAndType(user.ID, model.TokenTypeEmailChange)
	if err != nil {
		slog.Warn("failed to delete old email change tokens", "error", err, "user_id", user.ID)
	}

	user.PendingEmail = &newEmail
	err = s.userRepository.Update(user)
	if err != nil {
		return fmt.Errorf("failed to save pending email: %w", err)
	}

	verificationToken, err := s.GenerateToken()
	if err != nil {
		return err
	}

	token := &model.Token{
		UserID:    user.ID,
		Type:      model.TokenTypeEmailChange,
		TokenHash: hashToken(verificationToken),
		ExpiresAt: time.Now().Add(s.tokenEmailChangeExpiry),
	}
	err = s.tokenRepository.Create(token)
	if err != nil {
		return err
	}

	name := "User"
	if user.Name != "" {
		name = user.Name
	}

	err = s.emailService.SendEmailChangeVerification(newEmail, verificationToken, name)
	if err != nil {
		return fmt.Errorf("failed to send verification email: %w", err)
	}

	err = s.emailService.SendEmailChangeNotification(user.Email, newEmail, name)
	if err != nil {
		// Log error but don't fail the request
		slog.Warn("failed to send email change notification", "error", err, "user_id", user.ID)
	}

	return nil
}

// ConfirmEmailChange moves the pending address of the token's user to the
// account and ends every session of that user but the one of this
// request, when it is that user's, not impersonating: that browser stays
// in, with the session it had, and signedIn says so. The link never makes
// a session, so it is no way to sign in or to look freshly signed in.
// user is nil when the token is wrong, used or expired.
func (s *AuthService) ConfirmEmailChange(r *http.Request, token string) (user *model.User, signedIn bool, err error) {
	user, err = s.verifyEmailChange(token)
	if err != nil {
		return nil, false, err
	}
	session := ctxkeys.Session(r.Context())
	if session != nil && session.UserID == user.ID && !session.IsImpersonating() {
		return user, true, s.RevokeOtherSessions(user.ID, session.ID)
	}
	return user, false, s.RevokeAllSessions(user.ID)
}

// verifyEmailChange completes the email change after verification
func (s *AuthService) verifyEmailChange(token string) (*model.User, error) {
	// ConsumeToken atomically deletes the token, so only one request can use it.
	tokenModel, err := s.tokenRepository.ConsumeToken(hashToken(token))
	if err != nil {
		return nil, errors.New("invalid or expired verification link")
	}

	if tokenModel.Type != model.TokenTypeEmailChange {
		return nil, errors.New("invalid token type")
	}

	user, err := s.userRepository.ByID(tokenModel.UserID)
	if err != nil {
		return nil, err
	}

	// Check if pending email exists
	if user.PendingEmail == nil || *user.PendingEmail == "" {
		return nil, errors.New("no pending email change found")
	}

	// Move pending email to email
	user.Email = *user.PendingEmail
	user.PendingEmail = nil

	err = s.userRepository.Update(user)
	if err != nil {
		return nil, fmt.Errorf("failed to update email: %w", err)
	}

	return user, nil
}

// EnsureUser returns the user of email with a verified address, and
// creates the account when there is none and email may sign up. Only the
// operator calls it, from a server command, for their own address.
func (s *AuthService) EnsureUser(email string) (*model.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if err := validation.ValidateEmail(email); err != nil {
		return nil, ErrInvalidEmail
	}
	now := time.Now()
	user, err := s.userRepository.ByEmail(email)
	if err == nil {
		if user.EmailVerifiedAt == nil {
			user.EmailVerifiedAt = &now
			if err := s.userRepository.Update(user); err != nil {
				return nil, fmt.Errorf("verify the email: %w", err)
			}
		}
		return user, nil
	}
	if err := s.mayCreate(email); err != nil {
		return nil, err
	}
	user = &model.User{ID: uuid.New().String(), Email: email, EmailVerifiedAt: &now, CreatedAt: now}
	if err := s.userRepository.Create(user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	slog.Info("new user created", "user_id", user.ID)
	return user, nil
}

// CompleteOnboarding sets the user's name during onboarding and, when
// createWorkspace is set, makes the first workspace: named workspaceName,
// or "<name>'s Workspace" when it is empty.
func (s *AuthService) CompleteOnboarding(userID, name, workspaceName string, createWorkspace bool) (*model.Workspace, error) {
	err := validation.ValidateName(name)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	workspaceName = strings.TrimSpace(workspaceName)
	if createWorkspace && workspaceName != "" {
		err = validation.ValidateWorkspaceName(workspaceName)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidWorkspace, err)
		}
	}

	err = s.userRepository.UpdateName(userID, name, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to update user name: %w", err)
	}

	var workspace *model.Workspace
	if createWorkspace {
		workspace, err = s.workspaceService.EnsureFirst(userID, name, workspaceName)
		if err != nil {
			return nil, fmt.Errorf("failed to create workspace: %w", err)
		}
	}

	// Send welcome email now that we have the name
	user, err := s.userRepository.ByID(userID)
	if err == nil && workspace != nil && s.beta != nil {
		if err := s.beta.Unlock(user.Email, workspace.ID); err != nil {
			slog.Error("failed to unlock beta workspace", "error", err, "workspace_id", workspace.ID)
		}
	}
	if err == nil {
		err = s.emailService.SendWelcomeEmail(user.Email, name)
		if err != nil {
			slog.Warn("failed to send welcome email", "error", err)
		}
	}

	slog.Info("onboarding completed", "user_id", userID)
	return workspace, nil
}

// SetBeta closes sign-up to everyone the beta does not let in.
func (s *AuthService) SetBeta(beta *BetaService) {
	s.beta = beta
}

// mayCreate refuses a new address the beta does not let in.
func (s *AuthService) mayCreate(email string) error {
	if s.beta == nil {
		return nil
	}
	return s.beta.Admit(email)
}
