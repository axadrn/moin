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

// moin cloud runs a watcher for you. `moin cloud login` signs this laptop
// in through the browser; `moin cloud connect prod` has prod report there.
// The CLI talks to moin cloud over HTTPS with its session; it never gives
// moin cloud a way to a server.

// defaultCloudURL is moin cloud. MOIN_CLOUD_URL points the CLI at another
// one, like a site on your laptop: MOIN_CLOUD_URL=http://localhost:8090.
const defaultCloudURL = "https://" + build.Name + ".run"

// cloudURL is the moin cloud the CLI talks to.
func cloudURL() string {
	if address := build.Getenv("CLOUD_URL"); address != "" {
		return strings.TrimRight(address, "/")
	}
	return defaultCloudURL
}

// cloud is `moin cloud`, `moin cloud login`, `moin cloud connect <server>`,
// `moin cloud disconnect <server>`, and the copies in moin cloud
// (cloud_copies.go).
func cloud(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "login":
			return cloudLogin(ctx, args[1:], stdout)
		case "connect":
			return cloudConnect(ctx, args[1:], stdout)
		case "disconnect":
			return cloudDisconnect(ctx, args[1:], stdin, stdout)
		case "copies":
			return cloudCopies(ctx, args[1:], stdout)
		case "restore":
			return cloudRestore(ctx, args[1:], stdin, stdout)
		}
	}
	return cloudStatus(ctx, args, stdout)
}

// cloudSession is what the CLI keeps of its sign-in, in
// ~/.config/moin/cloud.json, only for you to read.
type cloudSession struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// cloudSessionFile is where the session lives.
func cloudSessionFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cloud.json"), nil
}

// errNotSignedIn says how to sign in.
var errNotSignedIn = fmt.Errorf("not signed in to %s cloud: run `%s cloud login`", build.Name, build.Name)

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
		return session, fmt.Errorf("%s: %w. Run `%s cloud login` again", file, err, build.Name)
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

// cloudCall sends body as JSON to path of moin cloud, with the session
// token when there is one, and reads the JSON answer into out. It returns
// the status code; an answer that is not 2xx is an error, but 202.
func cloudCall(ctx context.Context, method, path, token string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, cloudURL()+path, reader)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", build.Name+"/"+build.Version)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := cloudClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("%s cloud at %s: %w", build.Name, cloudURL(), err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnauthorized && token != "":
		return response.StatusCode, fmt.Errorf("your session at %s cloud ended: run `%s cloud login`", build.Name, build.Name)
	case response.StatusCode/100 != 2:
		answer, _ := io.ReadAll(io.LimitReader(response.Body, 300))
		return response.StatusCode, fmt.Errorf("%s cloud answered HTTP %d: %s", build.Name, response.StatusCode, strings.TrimSpace(string(answer)))
	case out != nil && response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusNoContent:
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
			return response.StatusCode, fmt.Errorf("%s cloud answered something else than JSON: %w", build.Name, err)
		}
	}
	return response.StatusCode, nil
}

// cloudLogin is `moin cloud login`: the browser signs in and allows the
// code this terminal shows; then the CLI gets its session.
func cloudLogin(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("cloud login", "cloud login")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	var login struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
		URL    string `json:"url"`
	}
	if _, err := cloudCall(ctx, http.MethodPost, "/api/cli/login", "", nil, &login); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Your code: "+style.Bold.Render(login.Code))
	step(stdout, "Allow it in the browser when the codes match: %s", login.URL)
	openBrowser(login.URL)

	var answer struct {
		Token string `json:"token"`
	}
	for {
		status, err := cloudCall(ctx, http.MethodPost, "/api/cli/login/finish", "", map[string]string{"secret": login.Secret}, &answer)
		if status == http.StatusGone {
			return errors.New("the login ran out after 10 minutes. Run it again")
		}
		if err != nil {
			return err
		}
		if status == http.StatusOK {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	session := cloudSession{URL: cloudURL(), Token: answer.Token}
	if err := saveCloudSession(session); err != nil {
		return err
	}
	var space cloudWorkspace
	if _, err := cloudCall(ctx, http.MethodGet, "/api/cloud", session.Token, nil, &space); err != nil {
		return err
	}
	done(stdout, "Signed in to %s cloud as %s", build.Name, space.Mail)
	hint(stdout, "Have a server report there:", build.Name+" cloud connect prod")
	return nil
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

// cloudWorkspace is what moin cloud answers `moin cloud`.
type cloudWorkspace struct {
	Mail      string     `json:"mail"`
	Status    string     `json:"status"`
	Plan      string     `json:"plan"`
	TrialEnds *time.Time `json:"trial_ends,omitempty"`
	PaidUntil *time.Time `json:"paid_until,omitempty"`
	// Billing is where to subscribe, or Stripe's portal once it knows you.
	Billing  string   `json:"billing"`
	NeedsYou []string `json:"needs_you"`
	Servers  []struct {
		Name    string `json:"name"`
		OK      bool   `json:"ok"`
		State   string `json:"state"`
		Version string `json:"version,omitempty"`
		Apps    int    `json:"apps"`
	} `json:"servers"`
}

// cloudStatus is `moin cloud`: the workspace, its plan, its servers.
func cloudStatus(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("cloud", "cloud [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unknown command %q. `%s cloud` takes login, connect, disconnect, copies or restore", flags.Arg(0), build.Name)
	}
	session, err := loadCloudSession()
	if err != nil {
		return err
	}
	var space cloudWorkspace
	if _, err := cloudCall(ctx, http.MethodGet, "/api/cloud", session.Token, nil, &space); err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, space)
	}
	fmt.Fprintln(stdout, style.Bold.Render(space.Mail)+"  "+style.Muted.Render(space.Plan))
	if space.Billing != "" {
		fmt.Fprintln(stdout, style.Muted.Render("Billing: "+space.Billing))
	}
	if len(space.Servers) == 0 {
		fmt.Fprintln(stdout, style.Muted.Render("No servers yet."))
		hint(stdout, "Have one report here:", build.Name+" cloud connect prod")
		return nil
	}
	fmt.Fprintln(stdout)
	rows := [][]string{}
	for _, server := range space.Servers {
		state := badge(stdout, style.Success.Render(style.SymbolLive)+" "+server.State, server.State)
		if !server.OK {
			state = badge(stdout, style.Danger.Render(style.SymbolFailed)+" "+server.State, server.State)
		}
		rows = append(rows, []string{server.Name, fmt.Sprint(server.Apps), server.Version, state})
	}
	table(stdout, []string{"SERVER", "APPS", "VERSION", "STATE"}, rows)
	if len(space.NeedsYou) > 0 {
		fmt.Fprintln(stdout)
		for _, problem := range space.NeedsYou {
			fmt.Fprintln(stdout, style.Danger.Render(style.SymbolFailed)+" "+problem)
		}
	}
	return nil
}

// cloudConnect is `moin cloud connect <server>`: moin cloud gives the
// server a token, and the server reports there from now on.
func cloudConnect(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("cloud connect", "cloud connect <server>")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	session, err := loadCloudSession()
	if err != nil {
		return err
	}
	var watcher daemon.Watcher
	if _, err := cloudCall(ctx, http.MethodPost, "/api/cloud/servers", session.Token, map[string]string{"name": serverName(server)}, &watcher); err != nil {
		return err
	}
	if err := sendJSON(ctx, server, watcher, "watchers", "connect"); err != nil {
		return fmt.Errorf("%s cloud waits for %s, but the server did not take the watcher: %w. Run it again", build.Name, server, err)
	}
	done(stdout, "%s reports to %s cloud every minute", server, build.Name)
	hint(stdout, "See it with", build.Name+" cloud")
	return nil
}

// cloudDisconnect is `moin cloud disconnect <server>`: moin cloud forgets
// the server, and the server stops reporting there.
func cloudDisconnect(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("cloud disconnect", "cloud disconnect <server> [-y]").withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	session, err := loadCloudSession()
	if err != nil {
		return err
	}
	question := fmt.Sprintf("Stop watching %s in %s cloud? Nobody gets a mail when it goes silent.", server, build.Name)
	if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
		return err
	}
	status, err := cloudCall(ctx, http.MethodDelete, "/api/cloud/servers/"+url.PathEscape(serverName(server)), session.Token, nil, nil)
	if err != nil && status != http.StatusNotFound {
		return err
	}
	if err := sshRun(ctx, server, hereLine("watchers", "disconnect", cloudURL()), nil, io.Discard, os.Stderr); err != nil {
		return fmt.Errorf("%s cloud no longer watches %s, but the server still has it as a watcher, its reports get no answer: %w", build.Name, server, err)
	}
	done(stdout, "%s no longer reports to %s cloud", server, build.Name)
	return nil
}
