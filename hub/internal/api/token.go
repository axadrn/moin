package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/internal/link"
)

// TokenHandler is the OAuth token endpoint and the revocation endpoint of
// the hub. They follow RFC 6749, RFC 8628 and RFC 7009 word for word, so
// any OAuth client works: the requests are forms, and the errors are
// {"error", "error_description"}, not the API's envelope.
type TokenHandler struct {
	logins      *service.DeviceLoginService
	connections *service.ConnectionService
	oauth       *service.OAuthService
}

func NewTokenHandler(logins *service.DeviceLoginService, connections *service.ConnectionService, oauth *service.OAuthService) *TokenHandler {
	return &TokenHandler{logins: logins, connections: connections, oauth: oauth}
}

const (
	// DeviceGrantType is the grant of RFC 8628: a poll with the device code.
	DeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	// RefreshGrantType exchanges a refresh token for new tokens.
	RefreshGrantType = "refresh_token"
	// CodeGrantType trades the code of an AI agent's approval, with its
	// PKCE verifier.
	CodeGrantType   = "authorization_code"
	maxTokenRequest = 8 << 10
)

// TokenResponse is a successful answer of POST /api/v1/auth/token (RFC
// 6749, section 5.1). Scope names the connection's rights, and
// ConnectionID the connection, so a client can tell it apart.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	ConnectionID string `json:"connection_id"`
}

// OAuthError is a failed answer of the token and revocation endpoints (RFC
// 6749, section 5.2).
type OAuthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// Token answers a poll of the device flow, the code of an AI agent or a
// refresh.
func (h *TokenHandler) Token(w http.ResponseWriter, r *http.Request) {
	if !readForm(w, r) {
		return
	}
	form := r.PostForm
	var tokens service.Tokens
	var err error
	switch form.Get("grant_type") {
	case DeviceGrantType:
		tokens, err = h.logins.Token(form.Get("device_code"))
	case CodeGrantType:
		tokens, err = h.oauth.Exchange(service.CodeExchange{
			Code:        form.Get("code"),
			Verifier:    form.Get("code_verifier"),
			RedirectURI: form.Get("redirect_uri"),
			ClientID:    form.Get("client_id"),
			Resource:    form.Get("resource"),
		})
	case RefreshGrantType:
		if resource := form.Get("resource"); resource != "" && !h.oauth.IsResource(resource) {
			err = service.ErrResourceInvalid
			break
		}
		tokens, err = h.connections.Refresh(form.Get("refresh_token"), form.Get("client_id"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "Use the device code, an authorization code or a refresh token.")
		return
	}
	switch {
	case errors.Is(err, service.ErrDeviceLoginPending):
		oauthError(w, http.StatusBadRequest, "authorization_pending", "Waiting for you to approve the login in the browser.")
	case errors.Is(err, service.ErrDeviceLoginSlowDown):
		oauthError(w, http.StatusBadRequest, "slow_down", "Polling too fast. Wait five seconds longer between polls.")
	case errors.Is(err, service.ErrDeviceLoginDenied):
		oauthError(w, http.StatusBadRequest, "access_denied", "The login was denied in the browser.")
	case errors.Is(err, service.ErrDeviceLoginExpired):
		oauthError(w, http.StatusBadRequest, "expired_token", "The code expired. Start the login again.")
	case errors.Is(err, service.ErrDeviceLoginInvalid):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The device code is unknown or was used already. Start the login again.")
	case errors.Is(err, service.ErrRefreshReused):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "This refresh token was used before, so the connection ended. Log in again.")
	case errors.Is(err, service.ErrCodeInvalid):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The authorization code is unknown, expired, used, or came with another client, redirect URI or code verifier. Sign in again.")
	case errors.Is(err, service.ErrResourceInvalid):
		oauthError(w, http.StatusBadRequest, "invalid_target", "The resource must be "+h.oauth.Resource()+".")
	case errors.Is(err, service.ErrRefreshInvalid):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The refresh token is unknown, expired or its connection ended. Log in again.")
	case err != nil:
		slog.Error("token endpoint failed", "error", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "Something went wrong. Please try again.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		JSON(w, http.StatusOK, TokenResponse{
			AccessToken:  tokens.Access,
			TokenType:    "Bearer",
			ExpiresIn:    int(tokens.ExpiresIn / time.Second),
			RefreshToken: tokens.Refresh,
			Scope:        strings.Join(tokens.Connection.Rights.List(), " "),
			ConnectionID: link.ID(tokens.Connection.ID),
		})
	}
}

// Revoke ends the connection of an access or refresh token (RFC 7009). It
// answers 200 for a token it does not know too: there is nothing left to
// end.
func (h *TokenHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	if !readForm(w, r) {
		return
	}
	token := r.PostForm.Get("token")
	if token == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Send the token to revoke.")
		return
	}
	if err := h.connections.Revoke(token, service.ClientSource(r.Context())); err != nil {
		slog.Error("revocation failed", "error", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "Something went wrong. Please try again.")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// readForm reads the form body of an OAuth request, which RFC 6749 asks
// for.
func readForm(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Send the request as application/x-www-form-urlencoded.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTokenRequest)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "The form could not be read.")
		return false
	}
	// A parameter sent twice is a mistake OAuth refuses (RFC 6749, section
	// 3.2).
	for name, values := range r.PostForm {
		if len(values) > 1 {
			oauthError(w, http.StatusBadRequest, "invalid_request", "Send "+name+" once.")
			return false
		}
	}
	return true
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	JSON(w, status, OAuthError{Error: code, Description: description})
}
