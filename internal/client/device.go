package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// DeviceLogin is a login in the browser that waits for the user: the code
// to check and the page to approve it on.
type DeviceLogin struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// pollUnit is one second of the poll interval; tests make it shorter.
var pollUnit = time.Second

// LoginInBrowser logs this computer in through the browser, the device
// flow of RFC 8628: it starts a login on the hub at baseURL, calls
// show with the code and the page to open, and polls until the user
// approved or denied it there, or it expired. surface is "cli" or "tui". The
// config it returns has the tokens of the new connection; the caller saves
// it with SaveConfig.
func LoginInBrowser(ctx context.Context, baseURL, surface string, show func(DeviceLogin)) (Config, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	err := CheckURL(baseURL)
	if err != nil {
		return Config{}, err
	}
	guest := &Client{url: baseURL, http: &http.Client{Timeout: 30 * time.Second}, surface: surface}
	hostname, _ := os.Hostname()
	var login DeviceLogin
	err = guest.do(ctx, http.MethodPost, "/api/v1/auth/device", struct {
		Hostname string `json:"hostname"`
	}{hostname}, &login)
	if err != nil {
		return Config{}, err
	}
	show(login)
	interval := max(login.Interval, 1)
	deadline := time.Now().Add(time.Duration(login.ExpiresIn) * pollUnit)
	for {
		if login.ExpiresIn > 0 && time.Now().After(deadline) {
			return Config{}, errLoginExpired
		}
		select {
		case <-ctx.Done():
			return Config{}, ctx.Err()
		case <-time.After(time.Duration(interval) * pollUnit):
		}
		config, err := exchange(ctx, guest.http, baseURL, surface, url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {login.DeviceCode}})
		var oauth *oauthError
		switch {
		case err == nil:
			return config, nil
		case !errors.As(err, &oauth):
			return Config{}, err
		case oauth.Code == "authorization_pending":
		case oauth.Code == "slow_down":
			interval += 5
		case oauth.Code == "access_denied":
			return Config{}, errors.New("the login was denied in the browser")
		case oauth.Code == "expired_token", oauth.Code == "invalid_grant":
			return Config{}, errLoginExpired
		default:
			return Config{}, err
		}
	}
}

var errLoginExpired = errors.New("the code expired before it was approved. Run `" + build.Name + " login` again")
