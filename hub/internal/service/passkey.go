package service

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

// PasskeyCeremonyExpiry is how long the browser has to answer the options
// of a passkey ceremony.
const PasskeyCeremonyExpiry = 5 * time.Minute

var (
	// ErrPasskeysOff says this hub has no passkeys: APP_URL is an IP
	// address, which no passkey belongs to.
	ErrPasskeysOff = errors.New("passkeys need APP_URL on a domain name")
	// ErrPasskeyExpired refuses a ceremony that is unknown, used or older
	// than PasskeyCeremonyExpiry.
	ErrPasskeyExpired = errors.New("this passkey request expired")
	// ErrPasskeyRefused refuses an answer that go-webauthn does not
	// verify, or a passkey this hub does not know.
	ErrPasskeyRefused = errors.New("this passkey did not work")
	// ErrImpersonating refuses a change to how someone signs in while an
	// operator impersonates them.
	ErrImpersonating = errors.New("impersonation is read-only")
)

// PasskeyService adds passkeys, signs in with them and removes them.
// go-webauthn does every check of a ceremony; this service keeps what it
// needs between the options and the answer, and the credentials.
type PasskeyService struct {
	passkeys *repository.Passkeys
	users    *repository.Users
	email    *EmailService
	// webAuthn is nil on a hub without passkeys (Config.Passkeys).
	webAuthn *webauthn.WebAuthn
	now      func() time.Time
}

func NewPasskeyService(passkeys *repository.Passkeys, users *repository.Users, email *EmailService, webAuthn *webauthn.WebAuthn) *PasskeyService {
	return &PasskeyService{passkeys: passkeys, users: users, email: email, webAuthn: webAuthn, now: time.Now}
}

// BeginSignIn starts a sign-in with any passkey of this hub: nobody types
// an email, the passkey names its user. It returns the options for the
// browser and the ceremony, which goes to SignIn with the answer.
func (s *PasskeyService) BeginSignIn() (*protocol.PublicKeyCredentialRequestOptions, string, error) {
	if s.webAuthn == nil {
		return nil, "", ErrPasskeysOff
	}
	assertion, session, err := s.webAuthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", err
	}
	ceremony, err := s.startCeremony(session)
	if err != nil {
		return nil, "", err
	}
	return &assertion.Response, ceremony, nil
}

// SignIn checks the browser's answer to the options of the ceremony and
// returns the user of the passkey. It stores the passkey's new sign
// counter, and refuses a passkey whose counter went back: a copy of it.
func (s *PasskeyService) SignIn(ceremony string, answer io.Reader) (*model.User, error) {
	session, err := s.takeCeremony(ceremony)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(answer)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeyRefused, err)
	}
	var user *model.User
	var passkeys []model.Passkey
	_, credential, err := s.webAuthn.ValidatePasskeyLogin(func(_, userHandle []byte) (webauthn.User, error) {
		found, err := s.users.ByID(string(userHandle))
		if err != nil {
			return nil, err
		}
		owner, rows, err := s.owner(found)
		user, passkeys = found, rows
		return owner, err
	}, *session, parsed)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeyRefused, err)
	}
	if credential.Authenticator.CloneWarning {
		slog.Warn("passkey sign counter went back: refused as a copy", "user_id", user.ID)
		return nil, fmt.Errorf("%w: the sign counter went back", ErrPasskeyRefused)
	}
	stored, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}
	for _, passkey := range passkeys {
		if bytes.Equal(passkey.CredentialID, credential.ID) {
			if err := s.passkeys.Used(passkey.ID, string(stored), s.now().UTC()); err != nil {
				return nil, err
			}
			return user, nil
		}
	}
	return nil, ErrPasskeyRefused
}

// BeginAdd starts adding a passkey to the user of session. It returns the
// options for the browser and the ceremony, which goes to Add with the
// answer. A passkey the user has already is left out.
func (s *PasskeyService) BeginAdd(session *model.Session) (*protocol.PublicKeyCredentialCreationOptions, string, error) {
	if session == nil || session.IsImpersonating() {
		return nil, "", ErrImpersonating
	}
	if s.webAuthn == nil {
		return nil, "", ErrPasskeysOff
	}
	user, err := s.users.ByID(session.UserID)
	if err != nil {
		return nil, "", err
	}
	owner, _, err := s.owner(user)
	if err != nil {
		return nil, "", err
	}
	creation, data, err := s.webAuthn.BeginRegistration(owner,
		webauthn.WithExclusions(webauthn.Credentials(owner.credentials).CredentialDescriptors()),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		return nil, "", err
	}
	ceremony, err := s.startCeremony(data)
	if err != nil {
		return nil, "", err
	}
	return &creation.Response, ceremony, nil
}

// Add checks the browser's answer to the options of the ceremony and
// stores the new passkey. Only the user who began the ceremony adds with it. An email tells
// the user, so a passkey nobody expected does not go unseen.
func (s *PasskeyService) Add(session *model.Session, ceremony string, answer io.Reader) (*model.Passkey, error) {
	if session == nil || session.IsImpersonating() {
		return nil, ErrImpersonating
	}
	data, err := s.takeCeremony(ceremony)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(answer)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeyRefused, err)
	}
	user, err := s.users.ByID(session.UserID)
	if err != nil {
		return nil, err
	}
	owner, _, err := s.owner(user)
	if err != nil {
		return nil, err
	}
	credential, err := s.webAuthn.CreateCredential(owner, *data, parsed)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeyRefused, err)
	}
	stored, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	passkey := &model.Passkey{
		ID:           uuid.New().String(),
		UserID:       user.ID,
		CredentialID: credential.ID,
		Credential:   string(stored),
		Name:         "Passkey",
		CreatedAt:    now,
	}
	if err := s.passkeys.Create(passkey); err != nil {
		return nil, err
	}
	slog.Info("passkey added", "user_id", user.ID)
	if err := s.email.SendPasskeyAdded(user.Email, user.Name, now); err != nil {
		slog.Warn("failed to send the passkey added email", "error", err, "user_id", user.ID)
	}
	return passkey, nil
}

// List is every passkey of the user, oldest first.
func (s *PasskeyService) List(userID string) ([]model.Passkey, error) {
	return s.passkeys.ByUser(userID)
}

// Remove deletes a passkey of the user of session. It refuses the last way
// in: the only passkey of a user without GitHub.
func (s *PasskeyService) Remove(session *model.Session, id string) error {
	if session == nil || session.IsImpersonating() {
		return ErrImpersonating
	}
	if err := s.passkeys.Delete(session.UserID, id); err != nil {
		return err
	}
	slog.Info("passkey removed", "user_id", session.UserID)
	return nil
}

// startCeremony keeps go-webauthn's session data of a ceremony for
// PasskeyCeremonyExpiry and returns its random id. Only the id's hash is
// stored; the id goes to the browser in a cookie.
func (s *PasskeyService) startCeremony(session *webauthn.SessionData) (string, error) {
	data, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	ceremony := base64.RawURLEncoding.EncodeToString(random)
	now := s.now().UTC()
	if err := s.passkeys.StartCeremony(hashToken(ceremony), string(data), now.Add(PasskeyCeremonyExpiry), now); err != nil {
		return "", err
	}
	return ceremony, nil
}

// takeCeremony uses up a ceremony and returns its session data.
func (s *PasskeyService) takeCeremony(ceremony string) (*webauthn.SessionData, error) {
	if s.webAuthn == nil {
		return nil, ErrPasskeysOff
	}
	if ceremony == "" {
		return nil, ErrPasskeyExpired
	}
	data, err := s.passkeys.TakeCeremony(hashToken(ceremony), s.now().UTC())
	if errors.Is(err, repository.ErrCeremonyNotFound) {
		return nil, ErrPasskeyExpired
	}
	if err != nil {
		return nil, err
	}
	session := &webauthn.SessionData{}
	if err := json.Unmarshal([]byte(data), session); err != nil {
		return nil, err
	}
	return session, nil
}

// owner is the user as go-webauthn asks for one, with their passkeys.
func (s *PasskeyService) owner(user *model.User) (passkeyOwner, []model.Passkey, error) {
	passkeys, err := s.passkeys.ByUser(user.ID)
	if err != nil {
		return passkeyOwner{}, nil, err
	}
	credentials := make([]webauthn.Credential, 0, len(passkeys))
	for _, passkey := range passkeys {
		var credential webauthn.Credential
		if err := json.Unmarshal([]byte(passkey.Credential), &credential); err != nil {
			return passkeyOwner{}, nil, fmt.Errorf("read passkey %s: %w", passkey.ID, err)
		}
		credentials = append(credentials, credential)
	}
	return passkeyOwner{user: user, credentials: credentials}, passkeys, nil
}

// passkeyOwner is a user as go-webauthn's webauthn.User. The user handle,
// which a passkey keeps and sends back on sign-in, is the user's ID.
type passkeyOwner struct {
	user        *model.User
	credentials []webauthn.Credential
}

func (o passkeyOwner) WebAuthnID() []byte                         { return []byte(o.user.ID) }
func (o passkeyOwner) WebAuthnName() string                       { return o.user.Email }
func (o passkeyOwner) WebAuthnCredentials() []webauthn.Credential { return o.credentials }

func (o passkeyOwner) WebAuthnDisplayName() string {
	if o.user.Name != "" {
		return o.user.Name
	}
	return o.user.Email
}
