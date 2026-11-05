package api

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/service"
)

// DeviceHandler is `moin login` in the browser, the device authorization
// grant of RFC 8628. Both endpoints are public: the device code is the
// credential of the poll.
type DeviceHandler struct {
	logins *service.DeviceLoginService
	appURL string
}

func NewDeviceHandler(logins *service.DeviceLoginService, appURL string) *DeviceHandler {
	return &DeviceHandler{logins: logins, appURL: strings.TrimRight(appURL, "/")}
}

type startDeviceLoginRequest struct {
	// Hostname names the computer, for the approval page and the token.
	Hostname string `json:"hostname"`
}

// DeviceLogin is the answer of POST /api/v1/auth/device, with the names of
// RFC 8628, section 3.2.
type DeviceLogin struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type deviceTokenRequest struct {
	DeviceCode string `json:"device_code"`
	// GrantType may name the grant of RFC 8628; nothing else is accepted.
	GrantType string `json:"grant_type,omitempty"`
}

// DeviceToken is the answer of POST /api/v1/auth/device/token once the
// login is approved. AccessToken is a personal API token of the kind device,
// shown this one time only.
type DeviceToken struct {
	AccessToken string   `json:"access_token"`
	TokenType   string   `json:"token_type"`
	ExpiresIn   int      `json:"expires_in"`
	APIToken    APIToken `json:"api_token"`
}

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// Start begins a login and answers the codes and where to confirm it.
func (h *DeviceHandler) Start(w http.ResponseWriter, r *http.Request) {
	var request startDeviceLoginRequest
	if !Decode(w, r, &request) {
		return
	}
	login, err := h.logins.Start(request.Hostname)
	if err != nil {
		ServiceError(w, err)
		return
	}
	verification := h.appURL + "/device"
	JSON(w, http.StatusOK, DeviceLogin{
		DeviceCode:              login.DeviceCode,
		UserCode:                login.UserCode,
		VerificationURI:         verification,
		VerificationURIComplete: verification + "?code=" + url.QueryEscape(login.UserCode),
		ExpiresIn:               int(time.Until(login.ExpiresAt).Round(time.Second).Seconds()),
		Interval:                login.Interval,
	})
}

// Token answers a poll: the token once the user approved, otherwise 400 with
// the error code of RFC 8628, section 3.5.
func (h *DeviceHandler) Token(w http.ResponseWriter, r *http.Request) {
	var request deviceTokenRequest
	if !Decode(w, r, &request) {
		return
	}
	if request.GrantType != "" && request.GrantType != deviceGrantType {
		Error(w, http.StatusBadRequest, "unsupported_grant_type", "Only the device code grant is supported.")
		return
	}
	token, raw, err := h.logins.Token(request.DeviceCode, service.ClientSource(r.Context()))
	switch {
	case errors.Is(err, service.ErrDeviceLoginPending):
		Error(w, http.StatusBadRequest, "authorization_pending", "Waiting for you to approve the login in the browser.")
	case errors.Is(err, service.ErrDeviceLoginSlowDown):
		Error(w, http.StatusBadRequest, "slow_down", "Polling too fast. Wait five seconds longer between polls.")
	case errors.Is(err, service.ErrDeviceLoginDenied):
		Error(w, http.StatusBadRequest, "access_denied", "The login was denied in the browser.")
	case errors.Is(err, service.ErrDeviceLoginExpired):
		Error(w, http.StatusBadRequest, "expired_token", "The code expired. Start the login again.")
	case errors.Is(err, service.ErrDeviceLoginInvalid):
		Error(w, http.StatusBadRequest, "invalid_grant", "The device code is unknown or was used already. Start the login again.")
	case err != nil:
		ServiceError(w, err)
	default:
		slog.Info("device token created", "token_id", token.ID, "user_id", token.UserID)
		expiresIn := 0
		if token.ExpiresAt != nil {
			expiresIn = int(time.Until(*token.ExpiresAt).Round(time.Second).Seconds())
		}
		JSON(w, http.StatusOK, DeviceToken{AccessToken: raw, TokenType: "Bearer", ExpiresIn: expiresIn, APIToken: NewAPIToken(*token, time.Now())})
	}
}
