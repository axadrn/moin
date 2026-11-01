// Package secret seals small values, such as app env vars, with AES-256-GCM.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// ErrOpen means a sealed value is broken, belongs to other context, or was
// sealed under another key.
var ErrOpen = errors.New("secret: cannot open sealed value")

// Box seals and opens values under one key.
type Box struct {
	aead cipher.AEAD
}

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("secret: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts value. context, such as "app id/key", binds the result to
// its place: opening it with another context fails.
func (b *Box) Seal(value, context string) string {
	nonce := make([]byte, b.aead.NonceSize())
	_, _ = rand.Read(nonce)
	sealed := b.aead.Seal(nonce, nonce, []byte(value), []byte(context))
	return base64.StdEncoding.EncodeToString(sealed)
}

// Open decrypts what Seal returned for the same context.
func (b *Box) Open(sealed, context string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrOpen
	}
	nonce, body := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	value, err := b.aead.Open(nil, nonce, body, []byte(context))
	if err != nil {
		return "", ErrOpen
	}
	return string(value), nil
}
