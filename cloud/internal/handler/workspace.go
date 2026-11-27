package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/axadrn/moin/cloud/internal/ui/pages"
	"github.com/axadrn/moin/cloud/internal/watchers"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/report"
	"github.com/axadrn/moin/internal/watcher"
)

// reportLimit is the largest report moin cloud reads.
const reportLimit = 1 << 20

// WorkspacePage is /cloud: the workspace, its plan, and what its watcher
// sees. It only reads.
func (c *Cloud) WorkspacePage(w http.ResponseWriter, r *http.Request) {
	space, ok := c.signedIn(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/cloud", http.StatusSeeOther)
		return
	}
	view, err := c.watchers.View(space.ID)
	if err != nil {
		renderError(w, "the watcher's view", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	render(w, r, pages.CloudWorkspace(pages.CloudWorkspaceProps{Mail: space.Mail, Plan: plan(space), View: view}))
}

// plan says in a few words what a workspace pays and until when.
func plan(space workspace.Workspace) string {
	switch {
	case space.Status == "paid" && !space.PaidUntil.IsZero():
		return "Paid until " + space.PaidUntil.Format("January 2, 2006")
	case space.Status == "paid":
		return "Paid"
	case space.Status == "ended":
		return "Ended"
	case space.TrialEnds.IsZero():
		return "Trial, starts with the first report"
	case space.TrialEnds.Before(time.Now()):
		return "Trial ended " + space.TrialEnds.Format("January 2, 2006")
	}
	return "Trial until " + space.TrialEnds.Format("January 2, 2006")
}

// cliWorkspace is what `moin cloud` shows.
type cliWorkspace struct {
	Mail      string      `json:"mail"`
	Status    string      `json:"status"`
	Plan      string      `json:"plan"`
	TrialEnds *time.Time  `json:"trial_ends,omitempty"`
	PaidUntil *time.Time  `json:"paid_until,omitempty"`
	NeedsYou  []string    `json:"needs_you"`
	Servers   []cliServer `json:"servers"`
}

type cliServer struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Apps    int    `json:"apps"`
}

// cliSession is the workspace of the session token a CLI sends, or a 401.
func (c *Cloud) cliSession(w http.ResponseWriter, r *http.Request) (workspace.Workspace, bool) {
	token, _ := bearer(r)
	space, err := c.store.SessionWorkspace(token)
	if err != nil {
		http.Error(w, "not signed in: run moin cloud login", http.StatusUnauthorized)
		return space, false
	}
	return space, true
}

// CLIWorkspace answers `moin cloud`.
func (c *Cloud) CLIWorkspace(w http.ResponseWriter, r *http.Request) {
	space, ok := c.cliSession(w, r)
	if !ok {
		return
	}
	view, err := c.watchers.View(space.ID)
	if err != nil {
		renderError(w, "the watcher's view", err)
		return
	}
	answer := cliWorkspace{Mail: space.Mail, Status: space.Status, Plan: plan(space), NeedsYou: view.NeedsYou, Servers: []cliServer{}}
	if answer.NeedsYou == nil {
		answer.NeedsYou = []string{}
	}
	if !space.TrialEnds.IsZero() {
		answer.TrialEnds = &space.TrialEnds
	}
	if !space.PaidUntil.IsZero() {
		answer.PaidUntil = &space.PaidUntil
	}
	for _, server := range view.Servers {
		answer.Servers = append(answer.Servers, cliServer{Name: server.Name, OK: server.OK, State: server.State, Version: server.Version, Apps: len(server.Apps)})
	}
	writeJSON(w, http.StatusOK, answer)
}

// ConnectServer gives a server of the CLI's workspace a token to report
// with, and says where to.
func (c *Cloud) ConnectServer(w http.ResponseWriter, r *http.Request) {
	space, ok := c.cliSession(w, r)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, "send the server's name", http.StatusBadRequest)
		return
	}
	if err := watcher.CheckName(body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token, err := c.store.ConnectServer(space.ID, body.Name)
	if err != nil {
		renderError(w, "connect a server", err)
		return
	}
	if err := c.watchers.Changed(space.ID); err != nil {
		renderError(w, "connect a server", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": c.appURL, "token": token})
}

// DisconnectServer forgets a server of the CLI's workspace.
func (c *Cloud) DisconnectServer(w http.ResponseWriter, r *http.Request) {
	space, ok := c.cliSession(w, r)
	if !ok {
		return
	}
	err := c.store.DisconnectServer(space.ID, r.PathValue("name"))
	if notFound(err) {
		http.Error(w, "no server "+r.PathValue("name")+" in moin cloud", http.StatusNotFound)
		return
	}
	if err == nil {
		err = c.watchers.Changed(space.ID)
	}
	if err != nil {
		renderError(w, "disconnect a server", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Report takes the report of a server. The token names the server; an
// unknown token gets 401, a report of another server 400. The answer is
// a status code and nothing a daemon could act on.
func (c *Cloud) Report(w http.ResponseWriter, r *http.Request) {
	token, ok := bearer(r)
	if !ok {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	var got report.Report
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, reportLimit)).Decode(&got); err != nil {
		http.Error(w, "not a report", http.StatusBadRequest)
		return
	}
	err := c.watchers.Receive(token, got)
	switch {
	case errors.Is(err, watchers.ErrUnknownToken):
		http.Error(w, "unknown token", http.StatusUnauthorized)
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
