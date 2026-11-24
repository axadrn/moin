package service

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
)

// RecoveryKeyExpiry is how long a recovery key works.
const RecoveryKeyExpiry = time.Hour

// ErrRecoveryKeyInvalid refuses a key that is wrong, used or expired.
var ErrRecoveryKeyInvalid = errors.New("this key is wrong, used or expired")

// CreateRecoveryKey makes a recovery key for the user of email, in place of
// one they had: 160 random bits, shown once, in groups of four. Only its
// hash is stored. It works once, for RecoveryKeyExpiry. The operator makes
// it with a server command, for the first sign-in of a self-hosted hub or
// for someone who lost their GitHub account or passkey. Signing in with it
// ends every other session, disconnects GitHub and deletes the passkeys: a
// recovery key resets every way in.
func (s *AuthService) CreateRecoveryKey(email string) (string, error) {
	user, err := s.userRepository.ByEmail(strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, repository.ErrUserNotFound) {
		return "", fmt.Errorf("no account has the email %s", email)
	}
	if err != nil {
		return "", err
	}
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.EncodeToString(random)
	groups := make([]string, 0, len(encoded)/4)
	for start := 0; start < len(encoded); start += 4 {
		groups = append(groups, encoded[start:start+4])
	}
	key := strings.Join(groups, "-")
	err = s.userRepository.SetRecoveryKey(user.ID, recoveryKeyHash(key), s.now().UTC().Add(RecoveryKeyExpiry))
	if err != nil {
		return "", err
	}
	return key, nil
}

// SignInWithRecoveryKey uses up a recovery key and returns its user, whose
// GitHub account it disconnects and whose passkeys it deletes: they connect
// their GitHub and add a passkey again.
func (s *AuthService) SignInWithRecoveryKey(key string) (*model.User, error) {
	userID, err := s.userRepository.UseRecoveryKey(recoveryKeyHash(key), s.now().UTC())
	if errors.Is(err, repository.ErrUserNotFound) {
		return nil, ErrRecoveryKeyInvalid
	}
	if err != nil {
		return nil, err
	}
	return s.userRepository.ByID(userID)
}

// recoveryKeyHash is the hash of a key as it is typed: case, spaces and
// dashes do not matter.
func recoveryKeyHash(key string) string {
	key = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(key)))
	return hashToken(key)
}
