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
	// ErrPasskeysOff says this hub has no passkeys: APP_URL is neither
	// https nor localhost, or go-webauthn takes no passkey for its host.
	ErrPasskeysOff = errors.New("passkeys need APP_URL on https with a domain name")
	// ErrPasskeyExpired refuses a ceremony that is unknown, used or older
	// than PasskeyCeremonyExpiry.
	ErrPasskeyExpired = errors.New("this passkey request expired")
	// ErrPasskeyRefused refuses an answer that go-webauthn does not
	// verify, or a passkey this hub does not know.
	ErrPasskeyRefused = errors.New("this passkey did not work")
	// ErrPasskeyName refuses a passkey name that is empty, longer than 64
	// characters or holds a control character.
	ErrPasskeyName = errors.New("passkey name must contain between 1 and 64 characters")
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
	sessions *repository.Sessions
	email    *EmailService
	// webAuthn is nil on a hub without passkeys (app.newPasskeys).
	webAuthn *webauthn.WebAuthn
	now      func() time.Time
}

func NewPasskeyService(passkeys *repository.Passkeys, users *repository.Users, sessions *repository.Sessions, email *EmailService, webAuthn *webauthn.WebAuthn) *PasskeyService {
	return &PasskeyService{passkeys: passkeys, users: users, sessions: sessions, email: email, webAuthn: webAuthn, now: time.Now}
}

// On says this hub offers passkeys. The sign-in page, the settings, the
// banner and the routes all ask this.
func (s *PasskeyService) On() bool {
	return s.webAuthn != nil
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
	if err := s.used(user, passkeys, credential); err != nil {
		return nil, err
	}
	return user, nil
}

// used stores the new sign counter of the passkey that just answered. It
// refuses a passkey whose counter went back, a copy of it, and one the user
// does not have.
func (s *PasskeyService) used(user *model.User, passkeys []model.Passkey, credential *webauthn.Credential) error {
	if credential.Authenticator.CloneWarning {
		slog.Warn("passkey sign counter went back: refused as a copy", "user_id", user.ID)
		return fmt.Errorf("%w: the sign counter went back", ErrPasskeyRefused)
	}
	stored, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	for _, passkey := range passkeys {
		if bytes.Equal(passkey.CredentialID, credential.ID) {
			return s.passkeys.Used(passkey.ID, string(stored), s.now().UTC())
		}
	}
	return ErrPasskeyRefused
}

// ConfirmedFor is how long a confirmation counts: adding a passkey,
// approving a connection and giving more rights need one this recent.
const ConfirmedFor = 5 * time.Minute

var (
	// ErrConfirm asks the person to confirm it is them: with a passkey or
	// with GitHub. A sign-in counts too. Nobody signs out for it.
	ErrConfirm = errors.New("confirm it's you: it counts for five minutes")
	// ErrPresencePasskey asks for the passkey: the account has one, so an
	// approval needs it, right then.
	ErrPresencePasskey = errors.New("allow with your passkey")
)

// Presence is the proof that the person is at the browser now, for an
// approval: the browser's answer to a passkey ceremony begun for it, or
// nothing when the session confirmed just now.
type Presence struct {
	Session  *model.Session
	Ceremony string
	Answer   []byte
}

// NeedsPasskey says whether the user approves with a passkey: they have
// one and this hub has passkeys.
func (s *PasskeyService) NeedsPasskey(userID string) (bool, error) {
	if s.webAuthn == nil {
		return false, nil
	}
	passkeys, err := s.passkeys.ByUser(userID)
	return len(passkeys) > 0, err
}

// BeginConfirm starts a ceremony in which the user of session confirms
// they are there: only their own passkeys answer, unlocked with a face, a
// finger or the screen lock.
func (s *PasskeyService) BeginConfirm(session *model.Session) (*protocol.PublicKeyCredentialRequestOptions, string, error) {
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
	assertion, data, err := s.webAuthn.BeginLogin(owner, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	ceremony, err := s.startCeremony(data)
	if err != nil {
		return nil, "", err
	}
	return &assertion.Response, ceremony, nil
}

// ConfirmPresence checks that the person of an approval is at the browser
// now. An account with a passkey answers a ceremony from BeginConfirm with
// it, right then (Confirm). An account without one, or on a hub without
// passkeys, must have confirmed in the last ConfirmedFor (Present).
func (s *PasskeyService) ConfirmPresence(presence Presence) error {
	if presence.Session == nil || presence.Session.IsImpersonating() {
		return ErrImpersonating
	}
	if len(presence.Answer) > 0 {
		return s.Confirm(presence)
	}
	needs, err := s.NeedsPasskey(presence.Session.UserID)
	if err != nil {
		return err
	}
	if needs {
		return ErrPresencePasskey
	}
	return s.Present(presence.Session)
}

// Confirm checks the passkey's answer to a ceremony from BeginConfirm;
// go-webauthn checks the answer, the user and user verification. Then the
// session counts as confirmed now. Impersonation never confirms.
func (s *PasskeyService) Confirm(presence Presence) error {
	session := presence.Session
	if session == nil || session.IsImpersonating() {
		return ErrImpersonating
	}
	data, err := s.takeCeremony(presence.Ceremony)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(presence.Answer))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPasskeyRefused, err)
	}
	user, err := s.users.ByID(session.UserID)
	if err != nil {
		return err
	}
	owner, passkeys, err := s.owner(user)
	if err != nil {
		return err
	}
	credential, err := s.webAuthn.ValidateLogin(owner, *data, parsed)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPasskeyRefused, err)
	}
	if err := s.used(user, passkeys, credential); err != nil {
		return err
	}
	now := s.now().UTC()
	if err := s.sessions.Confirm(session.ID, now); err != nil {
		return err
	}
	session.ConfirmedAt = now
	return nil
}

// Present is the one rule for what needs the person right now: the
// session confirmed it is them in the last ConfirmedFor, at sign-in, with
// a passkey (Confirm) or with GitHub (AuthService.ConfirmWithGitHub). Else
// ErrConfirm. Impersonation never is.
func (s *PasskeyService) Present(session *model.Session) error {
	if session == nil || session.IsImpersonating() {
		return ErrImpersonating
	}
	if s.now().Sub(session.ConfirmedAt) > ConfirmedFor {
		return ErrConfirm
	}
	return nil
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
	if err := s.Present(session); err != nil {
		return nil, "", err
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
// stores the new passkey, named after its password manager (passkeyMaker).
// Only the user who began the ceremony adds with it, and only while
// session is live and confirmed (Present): a passkey approves connections,
// so a stolen session must not add its own. An email tells the user, so a
// passkey nobody expected does not go unseen.
func (s *PasskeyService) Add(session *model.Session, ceremony string, answer io.Reader) (*model.Passkey, error) {
	if session == nil || session.IsImpersonating() {
		return nil, ErrImpersonating
	}
	if err := s.Present(session); err != nil {
		return nil, err
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
		Name:         passkeyMaker(credential.Authenticator.AAGUID),
		CreatedAt:    now,
	}
	if err := s.passkeys.Create(passkey, session.ID); err != nil {
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

// Rename names a passkey of the user of session. Another user's passkey
// is ErrPasskeyNotFound.
func (s *PasskeyService) Rename(session *model.Session, id, name string) (*model.Passkey, error) {
	if session == nil || session.IsImpersonating() {
		return nil, ErrImpersonating
	}
	name, err := passkeyName(name)
	if err != nil {
		return nil, err
	}
	passkey, err := s.passkeys.Rename(session.UserID, id, name)
	if err != nil {
		return nil, err
	}
	slog.Info("passkey renamed", "user_id", session.UserID)
	return passkey, nil
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
