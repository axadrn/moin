// Package github signs people in with the moin GitHub App: GitHub says
// who they are, moin cloud takes their verified primary mail.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNoMail is a GitHub account without a verified primary mail.
var ErrNoMail = errors.New("GitHub shared no verified primary email. Verify your email on GitHub and try again")

// GitHub is the client of the app that signs people in.
type GitHub struct {
	clientID     string
	clientSecret string
	webURL       string
	apiURL       string
	client       *http.Client
}

// New is the sign-in with the app's client ID and secret; webURL is
// https://github.com and apiURL https://api.github.com.
func New(clientID, clientSecret, webURL, apiURL string) *GitHub {
	return &GitHub{
		clientID:     clientID,
		clientSecret: clientSecret,
		webURL:       strings.TrimRight(webURL, "/"),
		apiURL:       strings.TrimRight(apiURL, "/"),
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// On says whether GitHub sign-in is set up.
func (g *GitHub) On() bool {
	return g.clientID != "" && g.clientSecret != ""
}

// AuthorizeURL is where a person signs in to GitHub. GitHub sends them
// back to back, one of the app's callback URLs, with a code and state.
func (g *GitHub) AuthorizeURL(state, back string) string {
	return g.webURL + "/login/oauth/authorize?" + url.Values{"client_id": {g.clientID}, "redirect_uri": {back}, "state": {state}}.Encode()
}

// Mail trades the code GitHub sent back for the verified primary mail of
// the account that signed in. It needs the app's account permission
// "Email addresses: Read".
func (g *GitHub) Mail(ctx context.Context, code, back string) (string, error) {
	var grant struct {
		AccessToken string `json:"access_token"`
	}
	form := map[string]string{"client_id": g.clientID, "client_secret": g.clientSecret, "code": code, "redirect_uri": back}
	if err := g.call(ctx, http.MethodPost, g.webURL+"/login/oauth/access_token", "", form, &grant); err != nil {
		return "", err
	}
	if grant.AccessToken == "" {
		return "", errors.New("GitHub refused the sign-in code")
	}
	var mails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := g.call(ctx, http.MethodGet, g.apiURL+"/user/emails", grant.AccessToken, nil, &mails); err != nil {
		return "", err
	}
	for _, mail := range mails {
		if mail.Primary && mail.Verified {
			return mail.Email, nil
		}
	}
	return "", ErrNoMail
}

// call sends body as JSON and reads the JSON answer into out.
func (g *GitHub) call(ctx context.Context, method, address, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := g.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("GitHub answered HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
}
