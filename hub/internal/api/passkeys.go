package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// PasskeysHandler lists and removes the passkeys of the signed-in user.
// Adding one is a ceremony in the browser: only the web adds passkeys.
type PasskeysHandler struct {
	passkeys *service.PasskeyService
}

func NewPasskeysHandler(passkeys *service.PasskeyService) *PasskeysHandler {
	return &PasskeysHandler{passkeys: passkeys}
}

// Passkey is the public JSON shape of a passkey. It never carries the
// credential.
type Passkey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// PasskeyList is the answer of GET /api/v1/account/passkeys.
type PasskeyList struct {
	Items []Passkey `json:"items"`
}

// List returns every passkey of the user, oldest first.
func (h *PasskeysHandler) List(w http.ResponseWriter, r *http.Request) {
	passkeys, err := h.passkeys.List(ctxkeys.User(r.Context()).ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	items := make([]Passkey, 0, len(passkeys))
	for _, passkey := range passkeys {
		items = append(items, newPasskey(passkey))
	}
	JSON(w, http.StatusOK, PasskeyList{Items: items})
}

// Remove deletes one passkey, unless it is the user's last way in.
func (h *PasskeysHandler) Remove(w http.ResponseWriter, r *http.Request) {
	if err := h.passkeys.Remove(ctxkeys.Session(r.Context()), r.PathValue("id")); err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}

func newPasskey(passkey model.Passkey) Passkey {
	var lastUsedAt *time.Time
	if passkey.LastUsedAt != nil {
		at := passkey.LastUsedAt.UTC()
		lastUsedAt = &at
	}
	return Passkey{ID: passkey.ID, Name: passkey.Name, CreatedAt: passkey.CreatedAt.UTC(), LastUsedAt: lastUsedAt}
}
