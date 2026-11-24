package service

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
)

const passkeyOrigin = "http://localhost:8090"

// softAuthenticator is a passkey in a test: a P-256 key that answers like
// a browser with a platform authenticator, with attestation none and the
// user verified. go-webauthn checks its answers as it checks real ones.
type softAuthenticator struct {
	key        *ecdsa.PrivateKey
	id         []byte
	userHandle []byte
	counter    uint32
}

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softAuthenticator{key: key, id: id}
}

// create answers the options of an add, as credential.toJSON() does.
func (a *softAuthenticator) create(t *testing.T, options *protocol.PublicKeyCredentialCreationOptions) io.Reader {
	t.Helper()
	a.userHandle = options.User.ID.(protocol.URLEncodedBase64)
	clientData := a.clientData(t, "webauthn.create", options.Challenge, passkeyOrigin)
	point, err := a.key.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	raw := point.Bytes()
	publicKey, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         1,
		XCoord:        raw[1:33],
		YCoord:        raw[33:65],
	})
	if err != nil {
		t.Fatal(err)
	}
	authData := a.authData(protocol.FlagUserPresent | protocol.FlagUserVerified | protocol.FlagAttestedCredentialData)
	authData = append(authData, make([]byte, 16)...) // AAGUID, zero
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(a.id)))
	authData = append(authData, a.id...)
	authData = append(authData, publicKey...)
	attestation, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	if err != nil {
		t.Fatal(err)
	}
	return a.answer(t, map[string]any{
		"clientDataJSON":    encode(clientData),
		"attestationObject": encode(attestation),
		"transports":        []string{"internal"},
	}, map[string]any{"credProps": map[string]any{"rk": true}})
}

// get answers the options of a sign-in from origin, signed with the next
// count.
func (a *softAuthenticator) get(t *testing.T, options *protocol.PublicKeyCredentialRequestOptions, origin string) io.Reader {
	t.Helper()
	a.counter++
	clientData := a.clientData(t, "webauthn.get", options.Challenge, origin)
	authData := a.authData(protocol.FlagUserPresent | protocol.FlagUserVerified)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return a.answer(t, map[string]any{
		"clientDataJSON":    encode(clientData),
		"authenticatorData": encode(authData),
		"signature":         encode(signature),
		"userHandle":        encode(a.userHandle),
	}, map[string]any{})
}

func (a *softAuthenticator) authData(flags protocol.AuthenticatorFlags) []byte {
	rpIDHash := sha256.Sum256([]byte("localhost"))
	data := append(rpIDHash[:], byte(flags))
	return binary.BigEndian.AppendUint32(data, a.counter)
}

func (a *softAuthenticator) clientData(t *testing.T, kind string, challenge protocol.URLEncodedBase64, origin string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"type": kind, "challenge": challenge.String(), "origin": origin, "crossOrigin": false})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (a *softAuthenticator) answer(t *testing.T, response, extensions map[string]any) io.Reader {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id":                      encode(a.id),
		"rawId":                   encode(a.id),
		"type":                    "public-key",
		"authenticatorAttachment": "platform",
		"response":                response,
		"clientExtensionResults":  extensions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(body)
}

func encode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

type passkeyEnvironment struct {
	authFlowEnvironment
	passkeys *PasskeyService
}

// newPasskeyEnvironment is a hub at passkeyOrigin with go-webauthn set up
// as app.go sets it up.
func newPasskeyEnvironment(t *testing.T) passkeyEnvironment {
	t.Helper()
	environment := newAuthFlowEnvironment(t)
	ceremony := webauthn.TimeoutConfig{Enforce: true, Timeout: PasskeyCeremonyExpiry, TimeoutUVD: PasskeyCeremonyExpiry}
	webAuthn, err := webauthn.New(&webauthn.Config{
		RPID:                  "localhost",
		RPDisplayName:         "moin",
		RPOrigins:             []string{passkeyOrigin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: ceremony, Registration: ceremony},
	})
	if err != nil {
		t.Fatal(err)
	}
	emailService := NewEmailService(environment.mailer, "http://localhost:8090", "Test App")
	passkeys := NewPasskeyService(repository.NewPasskeys(environment.database), environment.userRepository, emailService, webAuthn)
	return passkeyEnvironment{authFlowEnvironment: environment, passkeys: passkeys}
}

// add adds a passkey on authenticator to the user of session.
func (e passkeyEnvironment) add(t *testing.T, session *model.Session, authenticator *softAuthenticator) *model.Passkey {
	t.Helper()
	options, ceremony, err := e.passkeys.BeginAdd(session)
	if err != nil {
		t.Fatalf("BeginAdd() error = %v", err)
	}
	passkey, err := e.passkeys.Add(session, ceremony, authenticator.create(t, options))
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	return passkey
}

// signIn signs in with authenticator from origin.
func (e passkeyEnvironment) signIn(t *testing.T, authenticator *softAuthenticator, origin string) (*model.User, error) {
	t.Helper()
	options, ceremony, err := e.passkeys.BeginSignIn()
	if err != nil {
		t.Fatalf("BeginSignIn() error = %v", err)
	}
	return e.passkeys.SignIn(ceremony, authenticator.get(t, options, origin))
}

// A passkey added in the settings signs in, without an email typed. The
// user gets an email about it, and the hub counts it and when it was used.
func TestPasskeyAddsAndSignsIn(t *testing.T) {
	environment := newPasskeyEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Name: "Ada", Verified: true})
	authenticator := newSoftAuthenticator(t)

	passkey := environment.add(t, &model.Session{UserID: "ada"}, authenticator)
	if passkey.Name != "Passkey" || !bytes.Equal(passkey.CredentialID, authenticator.id) {
		t.Fatalf("passkey = %+v", passkey)
	}
	messages := environment.mailer.Messages()
	if len(messages) == 0 || messages[len(messages)-1].Subject != "A passkey was added to your account" || messages[len(messages)-1].To != "ada@example.com" {
		t.Fatalf("emails = %+v, want the passkey email", messages)
	}

	user, err := environment.signIn(t, authenticator, passkeyOrigin)
	if err != nil || user.ID != "ada" {
		t.Fatalf("SignIn() = %v, %v", user, err)
	}
	user, err = environment.signIn(t, authenticator, passkeyOrigin)
	if err != nil || user.ID != "ada" {
		t.Fatalf("second SignIn() = %v, %v", user, err)
	}
	listed, err := environment.passkeys.List("ada")
	if err != nil || len(listed) != 1 || listed[0].LastUsedAt == nil {
		t.Fatalf("List() = %+v, %v, want one used passkey", listed, err)
	}
	if user, err := environment.userRepository.ByID("ada"); err != nil || user.Passkeys != 1 {
		t.Fatalf("ByID() = %+v, %v, want one passkey counted", user, err)
	}
}

// A passkey answers for one site: an answer collected on another origin
// does not sign in.
func TestPasskeyRefusesAnotherOrigin(t *testing.T) {
	environment := newPasskeyEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Name: "Ada", Verified: true})
	authenticator := newSoftAuthenticator(t)
	environment.add(t, &model.Session{UserID: "ada"}, authenticator)

	if _, err := environment.signIn(t, authenticator, "https://phish.example.com"); !errors.Is(err, ErrPasskeyRefused) {
		t.Fatalf("another origin: error = %v, want ErrPasskeyRefused", err)
	}
}

// A ceremony works once, and an answer only for its own challenge.
func TestPasskeyRefusesAReplay(t *testing.T) {
	environment := newPasskeyEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Name: "Ada", Verified: true})
	authenticator := newSoftAuthenticator(t)
	environment.add(t, &model.Session{UserID: "ada"}, authenticator)

	options, ceremony, err := environment.passkeys.BeginSignIn()
	if err != nil {
		t.Fatal(err)
	}
	answer, err := io.ReadAll(authenticator.get(t, options, passkeyOrigin))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := environment.passkeys.SignIn(ceremony, bytes.NewReader(answer)); err != nil {
		t.Fatalf("first SignIn() error = %v", err)
	}
	if _, err := environment.passkeys.SignIn(ceremony, bytes.NewReader(answer)); !errors.Is(err, ErrPasskeyExpired) {
		t.Fatalf("the same ceremony again: error = %v, want ErrPasskeyExpired", err)
	}
	_, fresh, err := environment.passkeys.BeginSignIn()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := environment.passkeys.SignIn(fresh, bytes.NewReader(answer)); !errors.Is(err, ErrPasskeyRefused) {
		t.Fatalf("an old answer for a new ceremony: error = %v, want ErrPasskeyRefused", err)
	}
}

// A ceremony lasts five minutes.
func TestPasskeyCeremonyExpires(t *testing.T) {
	environment := newPasskeyEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Name: "Ada", Verified: true})
	authenticator := newSoftAuthenticator(t)
	environment.add(t, &model.Session{UserID: "ada"}, authenticator)

	options, ceremony, err := environment.passkeys.BeginSignIn()
	if err != nil {
		t.Fatal(err)
	}
	environment.passkeys.now = func() time.Time { return time.Now().Add(PasskeyCeremonyExpiry + time.Second) }
	if _, err := environment.passkeys.SignIn(ceremony, authenticator.get(t, options, passkeyOrigin)); !errors.Is(err, ErrPasskeyExpired) {
		t.Fatalf("after five minutes: error = %v, want ErrPasskeyExpired", err)
	}
	if _, err := environment.passkeys.SignIn("", nil); !errors.Is(err, ErrPasskeyExpired) {
		t.Fatalf("without a ceremony: error = %v, want ErrPasskeyExpired", err)
	}
}

// A sign counter that goes back means a copy of the passkey: refused.
func TestPasskeyRefusesALowerCounter(t *testing.T) {
	environment := newPasskeyEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Name: "Ada", Verified: true})
	authenticator := newSoftAuthenticator(t)
	environment.add(t, &model.Session{UserID: "ada"}, authenticator)
	for range 2 {
		if _, err := environment.signIn(t, authenticator, passkeyOrigin); err != nil {
			t.Fatal(err)
		}
	}

	authenticator.counter = 0
	if _, err := environment.signIn(t, authenticator, passkeyOrigin); !errors.Is(err, ErrPasskeyRefused) {
		t.Fatalf("a lower counter: error = %v, want ErrPasskeyRefused", err)
	}
}

// Only the user who began adding a passkey adds it, and nobody while an
// operator impersonates them.
func TestPasskeyAddBelongsToItsUser(t *testing.T) {
	environment := newPasskeyEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Name: "Ada", Verified: true})
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "bob", Email: "bob@example.com", Name: "Bob", Verified: true})
	authenticator := newSoftAuthenticator(t)

	options, ceremony, err := environment.passkeys.BeginAdd(&model.Session{UserID: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := environment.passkeys.Add(&model.Session{UserID: "bob"}, ceremony, authenticator.create(t, options)); !errors.Is(err, ErrPasskeyRefused) {
		t.Fatalf("Ada's ceremony for Bob: error = %v, want ErrPasskeyRefused", err)
	}
	operator := "operator"
	impersonation := &model.Session{UserID: "ada", ImpersonatorUserID: &operator}
	if _, _, err := environment.passkeys.BeginAdd(impersonation); !errors.Is(err, ErrImpersonating) {
		t.Fatalf("BeginAdd() while impersonating: error = %v", err)
	}
	if _, err := environment.passkeys.Add(impersonation, "x", nil); !errors.Is(err, ErrImpersonating) {
		t.Fatalf("Add() while impersonating: error = %v", err)
	}
	if err := environment.passkeys.Remove(impersonation, "x"); !errors.Is(err, ErrImpersonating) {
		t.Fatalf("Remove() while impersonating: error = %v", err)
	}
}

// The last way in stays: an account without GitHub keeps its last
// passkey. With GitHub, or another passkey, it goes.
func TestPasskeyKeepsTheLastWayIn(t *testing.T) {
	environment := newPasskeyEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Name: "Ada", Verified: true})
	session := &model.Session{UserID: "ada"}
	first := environment.add(t, session, newSoftAuthenticator(t))

	if err := environment.passkeys.Remove(session, first.ID); !errors.Is(err, repository.ErrPasskeyLastWayIn) {
		t.Fatalf("the last passkey: error = %v, want ErrPasskeyLastWayIn", err)
	}
	second := environment.add(t, session, newSoftAuthenticator(t))
	if err := environment.passkeys.Remove(session, first.ID); err != nil {
		t.Fatalf("one of two: error = %v", err)
	}
	if err := environment.passkeys.Remove(&model.Session{UserID: "bob"}, second.ID); !errors.Is(err, repository.ErrPasskeyNotFound) {
		t.Fatalf("someone else's passkey: error = %v, want ErrPasskeyNotFound", err)
	}
	if err := environment.auth.ConnectGitHub("ada", GitHubAccount{ID: 1, Login: "octocat"}); err != nil {
		t.Fatal(err)
	}
	if err := environment.passkeys.Remove(session, second.ID); err != nil {
		t.Fatalf("the last passkey with GitHub: error = %v", err)
	}
}

// A recovery key resets every way in: it also deletes the passkeys, so a
// stolen one no longer signs in.
func TestRecoveryKeyDeletesPasskeys(t *testing.T) {
	environment := newPasskeyEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Name: "Ada", Verified: true})
	authenticator := newSoftAuthenticator(t)
	environment.add(t, &model.Session{UserID: "ada"}, authenticator)
	key, err := environment.auth.CreateRecoveryKey("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}

	user, err := environment.auth.SignInWithRecoveryKey(key)
	if err != nil || user.Passkeys != 0 {
		t.Fatalf("SignInWithRecoveryKey() = %+v, %v, want no passkeys", user, err)
	}
	if passkeys, err := environment.passkeys.List("ada"); err != nil || len(passkeys) != 0 {
		t.Fatalf("List() = %v, %v, want none", passkeys, err)
	}
	if _, err := environment.signIn(t, authenticator, passkeyOrigin); !errors.Is(err, ErrPasskeyRefused) {
		t.Fatalf("the old passkey: error = %v, want ErrPasskeyRefused", err)
	}
}

// A hub at an IP address has no passkeys.
func TestPasskeysNeedADomain(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	passkeys := NewPasskeyService(repository.NewPasskeys(environment.database), environment.userRepository, nil, nil)
	if _, _, err := passkeys.BeginSignIn(); !errors.Is(err, ErrPasskeysOff) {
		t.Fatalf("BeginSignIn() error = %v, want ErrPasskeysOff", err)
	}
	if _, _, err := passkeys.BeginAdd(&model.Session{UserID: "ada"}); !errors.Is(err, ErrPasskeysOff) {
		t.Fatalf("BeginAdd() error = %v, want ErrPasskeysOff", err)
	}
}
