package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// A login in the browser is a connection: an access token for an hour and
// a refresh token that gets the next pair, once. The client refreshes by
// itself a minute before the hour ends, or when the hub no longer takes the
// access token. A refresh token used twice ends the connection, so two
// processes never refresh at once: a lock file next to the config keeps
// them in line, and the second takes the pair the first saved.

// refreshEarly is how long before its end an access token is refreshed.
const refreshEarly = time.Minute

// refreshTimeout is how long a refresh may take, the exchange and saving
// the pair. It is well below the half minute after which another process
// takes the lock as stale.
const refreshTimeout = 10 * time.Second

// ErrLoginEnded is a connection the hub ended: removed in Settings, unused
// for 90 days, or its refresh token used twice.
var ErrLoginEnded = errors.New("your login ended: run `" + build.Name + " login` again")

// token is the access token to send, refreshed first when its hour is
// nearly over.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	login := c.login
	c.mu.Unlock()
	if login.RefreshToken != "" && login.ExpiresAt != nil && time.Until(*login.ExpiresAt) < refreshEarly {
		if err := c.refresh(ctx, "Bearer "+login.Token); err != nil {
			return "", err
		}
		c.mu.Lock()
		login = c.login
		c.mu.Unlock()
	}
	return login.Token, nil
}

// refreshable reports whether the login can refresh: a connection, not a
// CI token.
func (c *Client) refreshable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.login.RefreshToken != ""
}

// refresh gets the next pair of tokens and saves it. sent is the
// Authorization the caller sent; when the login changed since, another
// request refreshed already and there is nothing to do.
func (c *Client) refresh(ctx context.Context, sent string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if "Bearer "+c.login.Token != sent {
		return nil
	}
	unlock, err := lockConfig(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	// Another process may have refreshed while this one waited: its pair is
	// in the file.
	if saved, err := LoadSaved(); err == nil && saved.URL == c.url && saved.Connection == c.login.Connection && saved.RefreshToken != "" && saved.RefreshToken != c.login.RefreshToken {
		c.login = saved
		return nil
	}
	// Once the hub has the refresh token it is used up, so the exchange and
	// saving the new pair finish even when the caller gives up: otherwise
	// the file keeps a used token, and the next refresh ends the login.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()
	client := &http.Client{Transport: c.http.Transport, Timeout: refreshTimeout}
	fresh, err := exchange(work, client, c.url, c.surface, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.login.RefreshToken}})
	if err != nil {
		var oauth *oauthError
		if errors.As(err, &oauth) && oauth.Code == "invalid_grant" {
			return ErrLoginEnded
		}
		return err
	}
	c.login = fresh
	c.config.Token, c.config.RefreshToken, c.config.ExpiresAt, c.config.Connection = fresh.Token, fresh.RefreshToken, fresh.ExpiresAt, fresh.Connection
	saved, err := LoadSaved()
	if err != nil {
		return err
	}
	saved.URL, saved.Token, saved.RefreshToken, saved.ExpiresAt, saved.Connection = c.url, fresh.Token, fresh.RefreshToken, fresh.ExpiresAt, fresh.Connection
	return SaveConfig(saved)
}

// tokenAnswer is what the token endpoint answers (RFC 6749, section 5.1).
type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	ConnectionID string `json:"connection_id"`
}

// oauthError is what the token endpoint answers when it gives no tokens
// (RFC 6749, section 5.2).
type oauthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *oauthError) Error() string {
	if e.Description != "" {
		return e.Description
	}
	return e.Code
}

// exchange asks the token endpoint of the hub at baseURL for tokens with
// form, without any login, and returns them as a config.
func exchange(ctx context.Context, client *http.Client, baseURL, surface string, form url.Values) (Config, error) {
	answer, err := postForm(ctx, client, baseURL+"/api/v1/auth/token", surface, form)
	if err != nil {
		return Config{}, err
	}
	var tokens tokenAnswer
	if err := json.Unmarshal(answer, &tokens); err != nil || tokens.AccessToken == "" {
		return Config{}, fmt.Errorf("the hub answered no token")
	}
	expires := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	return Config{URL: baseURL, Token: tokens.AccessToken, RefreshToken: tokens.RefreshToken, ExpiresAt: &expires, Connection: tokens.ConnectionID}, nil
}

// postForm sends an OAuth form and returns the body of a 200, or the
// oauthError of anything else.
func postForm(ctx context.Context, client *http.Client, address, surface string, form url.Values) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", build.Name+"-"+surface+"/"+build.Version)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusOK {
		return body, nil
	}
	var problem oauthError
	if json.Unmarshal(body, &problem) == nil && problem.Code != "" {
		return nil, &problem
	}
	return nil, fmt.Errorf("POST %s: %s", request.URL.Path, response.Status)
}

// Logout ends the connection of config at the hub, so its tokens stop
// working, before the config is forgotten. A CI token stays: it belongs to
// Settings.
func Logout(ctx context.Context, config Config) error {
	if config.RefreshToken == "" {
		return nil
	}
	_, err := postForm(ctx, &http.Client{Timeout: 30 * time.Second}, strings.TrimRight(config.URL, "/")+"/api/v1/auth/revoke", "cli", url.Values{"token": {config.RefreshToken}})
	return err
}

// lockConfig waits until this process may refresh the saved login, and
// returns what lets the next one in. A lock older than half a minute is
// from a process that died.
func lockConfig(ctx context.Context) (func(), error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lock := path + ".lock"
	deadline := time.Now().Add(15 * time.Second)
	for {
		file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(lock) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, err := os.Stat(lock); err == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("another " + build.Name + " is refreshing the login: try again")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
