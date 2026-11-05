package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/google/uuid"
)

const (
	// DeviceLoginLifetime is how long a code of `moin login` stays valid.
	DeviceLoginLifetime = 10 * time.Minute
	// DeviceLoginInterval is how long the CLI waits between polls, in
	// seconds. Polling faster adds deviceLoginSlowDown to it.
	DeviceLoginInterval = 5
	deviceLoginSlowDown = 5
	// deviceLoginLeeway forgives a poll that arrives a little early, as
	// requests take their time on the way.
	deviceLoginLeeway = time.Second
	// userCodeAlphabet has no letters or digits that look alike: no 0 and
	// O, no 1, I and L. Eight of them make about 8.5e11 codes.
	userCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	userCodeLength   = 8
	maxHostnameRunes = 50
)

// The answers of the token endpoint while there is no token, named as in
// RFC 8628, section 3.5.
var (
	ErrDeviceLoginPending  = errors.New("the login is not approved yet")
	ErrDeviceLoginSlowDown = errors.New("polled faster than the interval")
	ErrDeviceLoginDenied   = errors.New("the login was denied")
	ErrDeviceLoginExpired  = errors.New("the login expired")
	// ErrDeviceLoginInvalid is an unknown device code, or one that got its
	// token already.
	ErrDeviceLoginInvalid = errors.New("unknown device code")
	// ErrUserCodeNotFound is a user code that is wrong, expired or used.
	ErrUserCodeNotFound = errors.New("no pending login with this code")
)

// StartedDeviceLogin is what the CLI gets when a login starts: the device
// code it keeps and the user code the user confirms on the web.
type StartedDeviceLogin struct {
	DeviceCode string
	// UserCode is shown as ABCD-EFGH.
	UserCode  string
	ExpiresAt time.Time
	Interval  int
}

// DeviceLoginService runs `moin login` in the browser, the device
// authorization grant of RFC 8628: the CLI starts a login and polls, the
// user approves it on the web, and the next poll gets a device token.
type DeviceLoginService struct {
	logins *repository.DeviceLogins
	tokens *APITokenService
	now    func() time.Time
}

func NewDeviceLoginService(logins *repository.DeviceLogins, tokens *APITokenService) *DeviceLoginService {
	return &DeviceLoginService{logins: logins, tokens: tokens, now: time.Now}
}

// Start begins a login for the computer named hostname.
func (s *DeviceLoginService) Start(hostname string) (StartedDeviceLogin, error) {
	now := s.now().UTC()
	err := s.logins.DeleteExpired(now)
	if err != nil {
		slog.Warn("failed to delete expired device logins", "error", err)
	}
	deviceCode, err := randomHex(32)
	if err != nil {
		return StartedDeviceLogin{}, err
	}
	userCode, err := newUserCode()
	if err != nil {
		return StartedDeviceLogin{}, err
	}
	login := model.DeviceLogin{
		ID:              uuid.New().String(),
		DeviceCodeHash:  hashToken(deviceCode),
		UserCodeHash:    hashToken(userCode),
		Hostname:        cleanHostname(hostname),
		Status:          model.DeviceLoginPending,
		IntervalSeconds: DeviceLoginInterval,
		ExpiresAt:       now.Add(DeviceLoginLifetime),
		CreatedAt:       now,
	}
	err = s.logins.Create(login)
	if err != nil {
		return StartedDeviceLogin{}, err
	}
	return StartedDeviceLogin{DeviceCode: deviceCode, UserCode: FormatUserCode(userCode), ExpiresAt: login.ExpiresAt, Interval: login.IntervalSeconds}, nil
}

// Pending returns the pending login of a user code, as the user typed it:
// any case, with or without the dash and spaces.
func (s *DeviceLoginService) Pending(userCode string) (*model.DeviceLogin, error) {
	code := NormalizeUserCode(userCode)
	if len(code) != userCodeLength {
		return nil, ErrUserCodeNotFound
	}
	hash := hashToken(code)
	login, err := s.logins.ByUserCodeHash(hash)
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return nil, ErrUserCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(login.UserCodeHash), []byte(hash)) != 1 || login.Status != model.DeviceLoginPending || login.IsExpired(s.now()) {
		return nil, ErrUserCodeNotFound
	}
	return login, nil
}

// Approve lets the computer of a pending login act for the user in one of
// the user's workspaces. Its next poll gets the token.
func (s *DeviceLoginService) Approve(userID, userCode, workspaceID string) (*model.DeviceLogin, error) {
	login, err := s.Pending(userCode)
	if err != nil {
		return nil, err
	}
	err = s.tokens.requireMembership(userID, workspaceID)
	if err != nil {
		return nil, err
	}
	err = s.logins.Decide(login.ID, model.DeviceLoginApproved, userID, &workspaceID)
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return nil, ErrUserCodeNotFound
	}
	return login, err
}

// Deny turns a pending login down. Its next poll hears access_denied.
func (s *DeviceLoginService) Deny(userID, userCode string) (*model.DeviceLogin, error) {
	login, err := s.Pending(userCode)
	if err != nil {
		return nil, err
	}
	err = s.logins.Decide(login.ID, model.DeviceLoginDenied, userID, nil)
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return nil, ErrUserCodeNotFound
	}
	return login, err
}

// Token answers one poll of the CLI. Once the user approved, it creates the
// device token and returns it with the raw token, exactly once. Until then
// it returns ErrDeviceLoginPending, ErrDeviceLoginSlowDown when the CLI polls
// faster than the interval, ErrDeviceLoginDenied, ErrDeviceLoginExpired or
// ErrDeviceLoginInvalid.
func (s *DeviceLoginService) Token(deviceCode string, source Source) (*model.APIToken, string, error) {
	if deviceCode == "" {
		return nil, "", ErrDeviceLoginInvalid
	}
	hash := hashToken(deviceCode)
	login, err := s.logins.ByDeviceCodeHash(hash)
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return nil, "", ErrDeviceLoginInvalid
	}
	if err != nil {
		return nil, "", err
	}
	if subtle.ConstantTimeCompare([]byte(login.DeviceCodeHash), []byte(hash)) != 1 {
		return nil, "", ErrDeviceLoginInvalid
	}
	now := s.now().UTC()
	if login.IsExpired(now) {
		_ = s.logins.Delete(login.ID)
		return nil, "", ErrDeviceLoginExpired
	}
	interval := login.IntervalSeconds
	early := login.LastPolledAt != nil && now.Sub(*login.LastPolledAt) < time.Duration(interval)*time.Second-deviceLoginLeeway
	if early {
		interval += deviceLoginSlowDown
	}
	err = s.logins.Polled(login.ID, now, interval)
	if err != nil {
		return nil, "", err
	}
	if early {
		return nil, "", ErrDeviceLoginSlowDown
	}
	switch login.Status {
	case model.DeviceLoginDenied:
		_ = s.logins.Claim(login.ID, login.Status)
		return nil, "", ErrDeviceLoginDenied
	case model.DeviceLoginApproved:
	default:
		return nil, "", ErrDeviceLoginPending
	}
	err = s.logins.Claim(login.ID, model.DeviceLoginApproved)
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return nil, "", ErrDeviceLoginInvalid
	}
	if err != nil {
		return nil, "", err
	}
	if login.UserID == nil || login.WorkspaceID == nil {
		return nil, "", fmt.Errorf("approved device login %s has no user", login.ID)
	}
	return s.tokens.CreateForDevice(*login.UserID, *login.WorkspaceID, login.Hostname, source)
}

// NormalizeUserCode turns a typed user code into the stored form: upper
// case, without the dash and spaces.
func NormalizeUserCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}
		return r
	}, strings.TrimSpace(code))
}

// FormatUserCode shows a user code as ABCD-EFGH.
func FormatUserCode(code string) string {
	code = NormalizeUserCode(code)
	if len(code) != userCodeLength {
		return code
	}
	return code[:userCodeLength/2] + "-" + code[userCodeLength/2:]
}

func newUserCode() (string, error) {
	// 248 is the largest multiple of the alphabet's 31 letters below 256, so
	// every letter is as likely.
	limit := byte(256 - 256%len(userCodeAlphabet))
	code := make([]byte, 0, userCodeLength)
	buffer := make([]byte, 16)
	for len(code) < userCodeLength {
		_, err := rand.Read(buffer)
		if err != nil {
			return "", fmt.Errorf("generate user code: %w", err)
		}
		for _, b := range buffer {
			if b < limit && len(code) < userCodeLength {
				code = append(code, userCodeAlphabet[int(b)%len(userCodeAlphabet)])
			}
		}
	}
	return string(code), nil
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	_, err := rand.Read(buffer)
	if err != nil {
		return "", fmt.Errorf("generate device code: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

// cleanHostname keeps the computer's name printable and short.
func cleanHostname(hostname string) string {
	hostname = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(hostname))
	if runes := []rune(hostname); len(runes) > maxHostnameRunes {
		hostname = string(runes[:maxHostnameRunes])
	}
	return hostname
}
