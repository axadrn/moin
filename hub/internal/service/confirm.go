package service

import (
	"errors"
	"strings"
)

// ErrConfirmMismatch refuses a big step that cannot be undone, like removing
// an app, because confirm is not the name of what it changes. The service
// checks it, so the web, the API, the CLI and MCP share it (DESIGN.md,
// Asking first).
var ErrConfirmMismatch = errors.New("type the name to confirm")

// confirmed checks confirm, the name the user typed out, against name:
// the same after trimming spaces.
func confirmed(confirm, name string) error {
	if strings.TrimSpace(confirm) != name {
		return ErrConfirmMismatch
	}
	return nil
}
