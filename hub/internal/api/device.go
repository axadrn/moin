package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/service"
)

// DeviceHandler starts `moin login` in the browser, the device
// authorization grant of RFC 8628. It is public; the CLI then polls the
// token endpoint with the device code (TokenHandler).
type DeviceHandler struct {
	logins *service.DeviceLoginService
	appURL string
}

func NewDeviceHandler(logins *service.DeviceLoginService, appURL string) *DeviceHandler {
	return &DeviceHandler{logins: logins, appURL: strings.TrimRight(appURL, "/")}
}

type startDeviceLoginRequest struct {
	// Hostname names the computer, for the approval page and the
	// connection.
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
