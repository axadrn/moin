package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/internal/link"
)

// CITokensHandler lists, makes, edits and removes the CI tokens of a
// workspace. Making one and changing its rights need a signed-in browser.
type CITokensHandler struct {
	tokens   *service.CITokenService
	passkeys *service.PasskeyService
}

func NewCITokensHandler(tokens *service.CITokenService, passkeys *service.PasskeyService) *CITokensHandler {
	return &CITokensHandler{tokens: tokens, passkeys: passkeys}
}

// CIToken is the public JSON shape of a CI token. It never carries the
// token or its hash.
type CIToken struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Prefix is the start of the token after "pat_", for telling tokens
	// apart.
	Prefix string   `json:"prefix"`
	Rights []string `json:"rights"`
	// App is the one app the token reaches, null for every app.
	App *string `json:"app"`
	// CreatedBy is the name of the person who made it.
	CreatedBy   string     `json:"created_by"`
	WorkspaceID string     `json:"workspace_id"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Expired     bool       `json:"expired"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CITokenList is the answer of GET /api/v1/ci-tokens.
type CITokenList struct {
	Items []CIToken `json:"items"`
}

// CreatedCIToken is the answer of POST /api/v1/ci-tokens. Token is the raw
// token, shown this one time only.
type CreatedCIToken struct {
	Token   string  `json:"token"`
	CIToken CIToken `json:"ci_token"`
}

type createCITokenRequest struct {
	Name string `json:"name"`
	// Rights default to read, deploy and env.
	Rights *[]string `json:"rights"`
	// App limits the token to one app, by name or ID.
	App string `json:"app"`
	// ExpiresInDays is 1 to 365, or null for a token that never expires.
	ExpiresInDays *int `json:"expires_in_days"`
}

// NewCIToken converts a CI token into its public JSON shape.
func NewCIToken(token model.CIToken, now time.Time) CIToken {
	return CIToken{
		ID:          link.ID(token.ID),
		Name:        token.Name,
		Prefix:      token.Prefix,
		Rights:      token.Rights.List(),
		App:         token.AppName,
		CreatedBy:   token.MakerName,
		WorkspaceID: link.ID(token.WorkspaceID),
		LastUsedAt:  token.LastUsedAt,
		ExpiresAt:   token.ExpiresAt,
		Expired:     token.IsExpired(now),
		CreatedAt:   token.CreatedAt,
	}
}

// List returns the CI tokens of the workspace you see: yours, and as its
// owner every one.
func (h *CITokensHandler) List(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.tokens.List(ctxkeys.WorkspaceMember(r.Context()))
	if err != nil {
		ServiceError(w, err)
		return
	}
	now := time.Now()
	items := make([]CIToken, 0, len(tokens))
	for _, token := range tokens {
		items = append(items, NewCIToken(token, now))
	}
	JSON(w, http.StatusOK, CITokenList{Items: items})
}

// Create makes a CI token in the workspace and answers it once. It needs
// the person to have confirmed in the last five minutes, as an approval
// does.
func (h *CITokensHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request createCITokenRequest
	if !Decode(w, r, &request) {
		return
	}
	if err := h.passkeys.Present(ctxkeys.Session(r.Context())); err != nil {
		ServiceError(w, err)
		return
	}
	rights := model.DefaultRights
	if request.Rights != nil {
		parsed, ok := model.ParseRights(*request.Rights...)
		if !ok {
			ServiceError(w, service.ErrRightsUnknown)
			return
		}
		rights = parsed
	}
	input := service.NewCIToken{Name: request.Name, Rights: rights, App: request.App}
	if request.ExpiresInDays != nil {
		if *request.ExpiresInDays < 1 {
			ServiceError(w, service.ErrCITokenExpiry)
			return
		}
		input.ExpiresInDays = *request.ExpiresInDays
	}
	token, raw, err := h.tokens.Create(actor(r), input)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusCreated, CreatedCIToken{Token: raw, CIToken: NewCIToken(*token, time.Now())})
}

// SetRights changes what a CI token may do: {"rights": ["deploy"]}.
func (h *CITokensHandler) SetRights(w http.ResponseWriter, r *http.Request) {
	var request RightsRequest
	if !Decode(w, r, &request) {
		return
	}
	rights, ok := model.ParseRights(request.Rights...)
	if !ok {
		ServiceError(w, service.ErrRightsUnknown)
		return
	}
	token, err := h.tokens.SetRights(actor(r), r.PathValue("id"), rights, h.passkeys.Present(ctxkeys.Session(r.Context())))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewCIToken(*token, time.Now()))
}

// Remove deletes a CI token: yours, or as the owner any. Requests with it
// stop at once. A connection or CI token removes another person's only
// with the servers right: its own with read.
func (h *CITokensHandler) Remove(w http.ResponseWriter, r *http.Request) {
	member := ctxkeys.WorkspaceMember(r.Context())
	if ctxkeys.Bearer(r.Context()) != nil {
		tokens, err := h.tokens.List(member)
		if err != nil {
			ServiceError(w, err)
			return
		}
		for _, token := range tokens {
			if link.ID(token.ID) == link.ID(r.PathValue("id")) && token.UserID != member.UserID && !Allowed(w, r, model.RightServers) {
				return
			}
		}
	}
	err := h.tokens.Remove(member, r.PathValue("id"), source(r))
	if err != nil {
		ServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
