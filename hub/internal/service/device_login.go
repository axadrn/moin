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

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/internal/build"
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
// user approves it on the web with their passkey and picks the rights, and
// the next poll gets the tokens of the new connection.
type DeviceLoginService struct {
	logins      *repository.DeviceLogins
	connections *ConnectionService
	passkeys    *PasskeyService
	now         func() time.Time
}

func NewDeviceLoginService(logins *repository.DeviceLogins, connections *ConnectionService, passkeys *PasskeyService) *DeviceLoginService {
	return &DeviceLoginService{logins: logins, connections: connections, passkeys: passkeys, now: time.Now}
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

// Approval is a person letting the computer of a pending login in: in which
// workspace, with which rights, and the proof they are there now.
type Approval struct {
	UserCode    string
	WorkspaceID string
	Rights      model.Rights
	Presence    Presence
	Source      Source
}

// Approve makes the connection of a pending login, once the person proved
// they are there now (ConfirmPresence). The computer's next poll gets its
// tokens.
func (s *DeviceLoginService) Approve(approval Approval) (*model.DeviceLogin, *model.Connection, error) {
	login, err := s.Pending(approval.UserCode)
	if err != nil {
		return nil, nil, err
	}
	session := approval.Presence.Session
	if session == nil || session.IsImpersonating() {
		return nil, nil, ErrImpersonating
	}
	// The workspace and the rights first: a passkey is not spent on an
	// approval that fails anyway.
	if err := requireMembership(s.connections.workspaces, session.UserID, approval.WorkspaceID); err != nil {
		return nil, nil, err
	}
	if approval.Rights&^allRights() != 0 {
		return nil, nil, ErrRightsUnknown
	}
	if err := s.passkeys.ConfirmPresence(approval.Presence); err != nil {
		return nil, nil, err
	}
	connection, err := s.connections.Create(NewConnection{
		UserID:      approval.Presence.Session.UserID,
		WorkspaceID: approval.WorkspaceID,
		ClientName:  DeviceClientName(login.Hostname),
		Rights:      approval.Rights,
		Source:      approval.Source,
	})
	if err != nil {
		return nil, nil, err
	}
	err = s.logins.Decide(login.ID, model.DeviceLoginApproved, &connection.ID)
	if err != nil {
		// Another approval of the same code came first: this connection
		// goes again.
		_ = s.connections.endWith(*connection, approval.Source, "the code was approved twice")
	}
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return nil, nil, ErrUserCodeNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return login, connection, nil
}

// DeviceClientName names the connection of a computer: "moin CLI on
// <computer name>".
func DeviceClientName(hostname string) string {
	if hostname == "" {
		return build.Name + " CLI"
	}
	return CleanClientName(build.Name + " CLI on " + hostname)
}

// Deny turns a pending login down. Its next poll hears access_denied.
func (s *DeviceLoginService) Deny(userCode string) (*model.DeviceLogin, error) {
	login, err := s.Pending(userCode)
	if err != nil {
		return nil, err
	}
	err = s.logins.Decide(login.ID, model.DeviceLoginDenied, nil)
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return nil, ErrUserCodeNotFound
	}
	return login, err
}

// Token answers one poll of the CLI. Once the user approved, it returns the
// first tokens of the connection, exactly once. Until then it returns
// ErrDeviceLoginPending, ErrDeviceLoginSlowDown when the CLI polls faster
// than the interval, ErrDeviceLoginDenied, ErrDeviceLoginExpired or
// ErrDeviceLoginInvalid.
func (s *DeviceLoginService) Token(deviceCode string) (Tokens, error) {
	if deviceCode == "" {
		return Tokens{}, ErrDeviceLoginInvalid
	}
	hash := hashToken(deviceCode)
	login, err := s.logins.ByDeviceCodeHash(hash)
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return Tokens{}, ErrDeviceLoginInvalid
	}
	if err != nil {
		return Tokens{}, err
	}
	if subtle.ConstantTimeCompare([]byte(login.DeviceCodeHash), []byte(hash)) != 1 {
		return Tokens{}, ErrDeviceLoginInvalid
	}
	now := s.now().UTC()
	if login.IsExpired(now) {
		_ = s.logins.Delete(login.ID)
		return Tokens{}, ErrDeviceLoginExpired
	}
	interval := login.IntervalSeconds
	early := login.LastPolledAt != nil && now.Sub(*login.LastPolledAt) < time.Duration(interval)*time.Second-deviceLoginLeeway
	if early {
		interval += deviceLoginSlowDown
	}
	err = s.logins.Polled(login.ID, now, interval)
	if err != nil {
		return Tokens{}, err
	}
	if early {
		return Tokens{}, ErrDeviceLoginSlowDown
	}
	switch login.Status {
	case model.DeviceLoginDenied:
		_ = s.logins.Claim(login.ID, login.Status)
		return Tokens{}, ErrDeviceLoginDenied
	case model.DeviceLoginApproved:
	default:
		return Tokens{}, ErrDeviceLoginPending
	}
	err = s.logins.Claim(login.ID, model.DeviceLoginApproved)
	if errors.Is(err, repository.ErrDeviceLoginNotFound) {
		return Tokens{}, ErrDeviceLoginInvalid
	}
	if err != nil {
		return Tokens{}, err
	}
	if login.ConnectionID == nil {
		return Tokens{}, fmt.Errorf("approved device login %s has no connection", login.ID)
	}
	connection, err := s.connections.connections.ByID(*login.ConnectionID)
	if errors.Is(err, repository.ErrConnectionNotFound) {
		// Removed in Settings before the computer asked.
		return Tokens{}, ErrDeviceLoginDenied
	}
	if err != nil {
		return Tokens{}, err
	}
	return s.connections.Issue(connection)
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
