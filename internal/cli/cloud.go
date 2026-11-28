package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// moin cloud is the watcher we run. `moin watcher connect my-server` without
// an address has my-server report there: the browser signs this laptop in the
// first time. The CLI talks to moin cloud over HTTPS with its session; it
// never gives moin cloud a way to a server.

// defaultCloudURL is moin cloud. MOIN_CLOUD_URL points the CLI at another
// one, like a site on your laptop: MOIN_CLOUD_URL=http://localhost:8090.
const defaultCloudURL = "https://" + build.Name + ".run"

// cloudName is how the CLI names moin cloud, like in the WHERE of
// `moin backups`.
const cloudName = build.Name + " cloud"

// cloudURL is the moin cloud the CLI talks to.
func cloudURL() string {
	if address := build.Getenv("CLOUD_URL"); address != "" {
		return strings.TrimRight(address, "/")
	}
	return defaultCloudURL
}

// cloudSession is this laptop's sign-in to moin cloud, in
// ~/.config/moin/cloud.json, only for you to read. With it the CLI
// connects servers, and lists and restores the copies moin cloud keeps.
type cloudSession struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// cloudSessionFile is where the session of moin cloud lives.
func cloudSessionFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cloud.json"), nil
}

// errNotSignedIn says how to sign in.
var errNotSignedIn = fmt.Errorf("this laptop is not signed in to %s. `%s watcher connect <server>` signs it in", cloudName, build.Name)

// errSessionEnded is a session that moin cloud no longer knows.
var errSessionEnded = fmt.Errorf("your session at %s ended. `%s watcher connect <server>` signs this laptop in again", cloudName, build.Name)

// loadCloudSession is the session for the moin cloud the CLI talks to.
func loadCloudSession() (cloudSession, error) {
	var session cloudSession
	file, err := cloudSessionFile()
	if err != nil {
		return session, err
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return session, errNotSignedIn
	}
	if err != nil {
		return session, err
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return session, fmt.Errorf("%s: %w. Remove it and sign in again with `%s watcher connect <server>`", file, err, build.Name)
	}
	if session.URL != cloudURL() || session.Token == "" {
		return session, errNotSignedIn
	}
	return session, nil
}

// saveCloudSession keeps the session, readable only by you.
func saveCloudSession(session cloudSession) error {
	file, err := cloudSessionFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	temp := file + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, file)
}

// cloudClient talks to moin cloud.
var cloudClient = &http.Client{Timeout: 30 * time.Second}

// call sends body as JSON to path of moin cloud, with the session token
// when there is one, and reads the JSON answer into out. It returns the
// status code; an answer that is not 2xx is an error, but 202.
func (session cloudSession) call(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, session.URL+path, reader)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", build.Name+"/"+build.Version)
	if session.Token != "" {
		request.Header.Set("Authorization", "Bearer "+session.Token)
	}
	response, err := cloudClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("%s at %s: %w", cloudName, session.URL, err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnauthorized && session.Token != "":
		return response.StatusCode, errSessionEnded
	case response.StatusCode/100 != 2:
		answer, _ := io.ReadAll(io.LimitReader(response.Body, 300))
		return response.StatusCode, fmt.Errorf("%s answered HTTP %d: %s", cloudName, response.StatusCode, strings.TrimSpace(string(answer)))
	case out != nil && response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusNoContent:
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
			return response.StatusCode, fmt.Errorf("%s answered something else than JSON: %w", cloudName, err)
		}
	}
	return response.StatusCode, nil
}

// cloudCall is a call to moin cloud with this laptop's session. Without
// one, or when it ended, the browser signs this laptop in first.
func cloudCall(ctx context.Context, stdout io.Writer, method, path string, body, out any) (int, error) {
	session, err := loadCloudSession()
	status := 0
	if err == nil {
		status, err = session.call(ctx, method, path, body, out)
	}
	if errors.Is(err, errNotSignedIn) || errors.Is(err, errSessionEnded) {
		if session, err = cloudSignIn(ctx, stdout); err != nil {
			return 0, err
		}
		status, err = session.call(ctx, method, path, body, out)
	}
	return status, err
}

// cloudSignIn signs this laptop in to moin cloud: the browser signs in and
// allows the code this terminal shows; then the CLI gets its session.
func cloudSignIn(ctx context.Context, stdout io.Writer) (cloudSession, error) {
	session := cloudSession{URL: cloudURL()}
	var login struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
		URL    string `json:"url"`
	}
	if _, err := session.call(ctx, http.MethodPost, "/api/cli/login", nil, &login); err != nil {
		return session, err
	}
	step(stdout, "Sign in to %s in the browser, and allow the code %s: %s", cloudName, style.Bold.Render(login.Code), login.URL)
	// The browser opens only a page of moin cloud, never another address
	// or scheme an answer names.
	if strings.HasPrefix(login.URL, session.URL+"/") {
		openBrowser(login.URL)
	}

	var answer struct {
		Token string `json:"token"`
	}
	for {
		status, err := session.call(ctx, http.MethodPost, "/api/cli/login/finish", map[string]string{"secret": login.Secret}, &answer)
		if status == http.StatusGone {
			return session, errors.New("the sign-in ran out after 10 minutes. Run it again")
		}
		if err != nil {
			return session, err
		}
		if status == http.StatusOK {
			break
		}
		select {
		case <-ctx.Done():
			return session, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	session.Token = answer.Token
	if err := saveCloudSession(session); err != nil {
		return session, err
	}
	done(stdout, "Signed in to %s", cloudName)
	return session, nil
}

// openBrowser opens address in the browser, when this machine has one.
// The address is on the screen anyway.
func openBrowser(address string) {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	command := exec.Command(opener, address)
	if command.Start() == nil {
		go func() { _ = command.Wait() }()
	}
}

// cloudWorkspace is what moin cloud says of your workspace, for
// `moin watcher`.
type cloudWorkspace struct {
	Mail      string     `json:"mail"`
	Status    string     `json:"status"`
	Plan      string     `json:"plan"`
	TrialEnds *time.Time `json:"trial_ends,omitempty"`
	PaidUntil *time.Time `json:"paid_until,omitempty"`
	// TrialDaysLeft is counted here, from TrialEnds.
	TrialDaysLeft int `json:"trial_days_left,omitempty"`
	// Billing is where to subscribe, or Stripe's portal once it knows you.
	Billing string `json:"billing"`
	// Storage is how many bytes the copies in moin cloud take, of
	// StorageLimit.
	Storage      int64 `json:"storage"`
	StorageLimit int64 `json:"storage_limit"`
}

// trialDaysLeft is how many days of the trial are left, 0 when it is not
// a trial or it ended. A day begun counts.
func trialDaysLeft(space cloudWorkspace, now time.Time) int {
	if space.Status == "paid" || space.TrialEnds == nil || !space.TrialEnds.After(now) {
		return 0
	}
	return int((space.TrialEnds.Sub(now) + 24*time.Hour - 1) / (24 * time.Hour))
}

// cloudConnect has server report to moin cloud: moin cloud gives it a
// token, and the server takes moin cloud as a watcher.
func cloudConnect(ctx context.Context, server string, stdout io.Writer) error {
	var watcher daemon.Watcher
	if _, err := cloudCall(ctx, stdout, http.MethodPost, "/api/cloud/servers", map[string]string{"name": serverName(server)}, &watcher); err != nil {
		return err
	}
	if err := sendJSON(ctx, server, watcher, "watchers", "connect"); err != nil {
		return fmt.Errorf("%s waits for %s, but the server did not take the watcher: %w. Run it again", cloudName, server, err)
	}
	done(stdout, "%s reports to %s every minute", server, cloudName)
	hint(stdout, "See it with", build.Name+" watcher")
	return nil
}

// cloudDisconnect has moin cloud forget server, and the server stop
// reporting there.
func cloudDisconnect(ctx context.Context, server string, yes bool, stdin io.Reader, stdout io.Writer) error {
	question := fmt.Sprintf("Stop watching %s in %s? Nobody gets a mail when it goes silent.", server, cloudName)
	if err := confirm(stdin, stdout, yes, question); err != nil {
		return err
	}
	status, err := cloudCall(ctx, stdout, http.MethodDelete, "/api/cloud/servers/"+url.PathEscape(serverName(server)), nil, nil)
	if err != nil && status != http.StatusNotFound {
		return err
	}
	if err := sshRun(ctx, server, hereLine("watchers", "disconnect", cloudURL()), nil, io.Discard, os.Stderr); err != nil {
		return fmt.Errorf("%s no longer watches %s, but the server still has it as a watcher, its reports get no answer: %w", cloudName, server, err)
	}
	done(stdout, "%s no longer reports to %s", server, cloudName)
	return nil
}
