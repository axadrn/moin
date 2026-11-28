package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/axadrn/moin/cloud/internal/ui/pages"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

// The sign-in of `moin watcher connect <server>`: the CLI starts a login and gets a code, opens
// /cli?code=… in the browser, and waits. The browser signs in, shows the
// same code, and one button allows it. Then the CLI gets its session.

// cliLogin is the answer to a new sign-in of the CLI.
type cliLogin struct {
	Secret string `json:"secret"`
	Code   string `json:"code"`
	URL    string `json:"url"`
}

// StartCLILogin starts a sign-in of the CLI.
func (c *Cloud) StartCLILogin(w http.ResponseWriter, r *http.Request) {
	if !c.ipLimit.allow(clientIP(r), time.Now()) {
		http.Error(w, "too many logins in a short time, wait 15 minutes", http.StatusTooManyRequests)
		return
	}
	secret, code, err := c.store.NewCLILogin()
	if err != nil {
		renderError(w, "start a cli login", err)
		return
	}
	writeJSON(w, http.StatusOK, cliLogin{Secret: secret, Code: code, URL: c.appURL + "/cli?" + url.Values{"code": {code}}.Encode()})
}

// FinishCLILogin answers the waiting CLI: 202 while no browser allowed
// it, then its session token once, then 410.
func (c *Cloud) FinishCLILogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, "not a login", http.StatusBadRequest)
		return
	}
	token, err := c.store.FinishCLILogin(body.Secret)
	switch {
	case errors.Is(err, workspace.ErrWaiting):
		w.WriteHeader(http.StatusAccepted)
	case notFound(err):
		http.Error(w, "this login ran out or was used: run the login again", http.StatusGone)
	case err != nil:
		renderError(w, "finish a cli login", err)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"token": token})
	}
}

// CLIPage shows the code of a sign-in of the CLI to compare with the
// terminal, and the button that allows it.
func (c *Cloud) CLIPage(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	space, ok := c.signedIn(r)
	if !ok {
		http.Redirect(w, r, "/login?"+url.Values{"next": {"/cli?" + url.Values{"code": {code}}.Encode()}}.Encode(), http.StatusSeeOther)
		return
	}
	if !c.store.CLILoginWaits(code) {
		w.WriteHeader(http.StatusGone)
		render(w, r, pages.CloudLoginFailed("This sign-in ran out or was used. Run moin watcher connect again."))
		return
	}
	render(w, r, pages.CloudCLI(code, space.Mail))
}

// AllowCLI lets a sign-in of the CLI into the browser's workspace.
func (c *Cloud) AllowCLI(w http.ResponseWriter, r *http.Request) {
	space, ok := c.signedIn(r)
	if !ok || !c.sameOrigin(r) {
		http.Error(w, "sign in first", http.StatusForbidden)
		return
	}
	err := c.store.AllowCLILogin(r.PostFormValue("code"), space.Mail)
	if notFound(err) {
		w.WriteHeader(http.StatusGone)
		render(w, r, pages.CloudLoginFailed("This sign-in ran out or was used. Run moin watcher connect again."))
		return
	}
	if err != nil {
		renderError(w, "allow a cli login", err)
		return
	}
	render(w, r, pages.CloudCLIAllowed())
}

// writeJSON answers value as JSON with status.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
