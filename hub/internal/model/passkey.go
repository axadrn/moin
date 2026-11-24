package model

import "time"

// Passkey is a way to sign in next to GitHub: a key pair on the user's
// device or in their password manager. The hub keeps only the public key.
type Passkey struct {
	ID     string `db:"id"`
	UserID string `db:"user_id"`
	// CredentialID is the id the browser sends back on sign-in.
	CredentialID []byte `db:"credential_id"`
	// Credential is the JSON of go-webauthn's webauthn.Credential: the
	// public key, the sign counter and the flags.
	Credential string `db:"credential"`
	// Name says which passkey it is, like "iCloud Keychain".
	Name       string     `db:"name"`
	LastUsedAt *time.Time `db:"last_used_at"`
	CreatedAt  time.Time  `db:"created_at"`
}
