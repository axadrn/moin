package handler

import (
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/axadrn/moin/cloud/internal/copies"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/watcher"
)

// The pages of a signed-in workspace. They only read: nothing on them
// changes a server.

// copiesShown is how many copies /cloud/copies lists before "Show all".
const copiesShown = 20

// page is the workspace a browser is signed in to, and what the bar of
// every page shows of it. A browser that is not signed in goes to the
// sign-in, and back here after it.
func (c *Cloud) page(w http.ResponseWriter, r *http.Request) (workspace.Workspace, pages.Cloud, bool) {
	space, ok := c.signedIn(r)
	if !ok {
		http.Redirect(w, r, "/login?"+url.Values{"next": {r.URL.RequestURI()}}.Encode(), http.StatusSeeOther)
		return space, pages.Cloud{}, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return space, pages.Cloud{Mail: space.Mail, Plan: pages.PlanOf(space, time.Now(), c.Stripe != nil)}, true
}

// view is what the watcher of a workspace sees.
func (c *Cloud) view(w http.ResponseWriter, space workspace.Workspace) (watcher.View, bool) {
	view, err := c.watchers.View(space.ID)
	if err != nil {
		renderError(w, "the watcher's view", err)
		return view, false
	}
	return view, true
}

// WorkspacePage is /cloud: what needs you, the servers, the alerts and
// the last proven restores.
func (c *Cloud) WorkspacePage(w http.ResponseWriter, r *http.Request) {
	space, frame, ok := c.page(w, r)
	if !ok {
		return
	}
	view, ok := c.view(w, space)
	if !ok {
		return
	}
	render(w, r, pages.CloudOverview(pages.CloudOverviewProps{Cloud: frame, View: view}))
}

// ServerPage is /cloud/servers/{name}: one server, its apps, databases,
// alerts and knocks.
func (c *Cloud) ServerPage(w http.ResponseWriter, r *http.Request) {
	space, frame, ok := c.page(w, r)
	if !ok {
		return
	}
	view, ok := c.view(w, space)
	if !ok {
		return
	}
	name := r.PathValue("name")
	i := slices.IndexFunc(view.Servers, func(s watcher.ServerView) bool { return s.Name == name })
	if i < 0 {
		NotFound(w, r)
		return
	}
	var outages []watcher.OutageView
	for _, o := range view.Outages {
		if o.Server == name {
			outages = append(outages, o)
		}
	}
	var checks []watcher.CheckView
	for _, check := range view.Checks {
		if check.Server == name {
			checks = append(checks, check)
		}
	}
	render(w, r, pages.CloudServer(pages.CloudServerProps{Cloud: frame, Server: view.Servers[i], Outages: outages, Checks: checks}))
}

// AlertsPage is /cloud/alerts: every outage and restore check the
// watcher keeps.
func (c *Cloud) AlertsPage(w http.ResponseWriter, r *http.Request) {
	space, frame, ok := c.page(w, r)
	if !ok {
		return
	}
	view, ok := c.view(w, space)
	if !ok {
		return
	}
	render(w, r, pages.CloudAlerts(pages.CloudAlertsProps{Cloud: frame, View: view}))
}

// CopiesPage is /cloud/copies: the copies the servers sent, newest
// first, of one server with ?server=, all of them with ?all.
func (c *Cloud) CopiesPage(w http.ResponseWriter, r *http.Request) {
	space, frame, ok := c.page(w, r)
	if !ok {
		return
	}
	server := r.URL.Query().Get("server")
	every, err := c.store.Copies(space.ID, "", "")
	if err != nil {
		renderError(w, "list the copies", err)
		return
	}
	stored, err := c.store.Storage(space.ID)
	if err != nil {
		renderError(w, "the storage of the copies", err)
		return
	}
	props := pages.CloudCopiesProps{Cloud: frame, Stored: stored, Limit: copies.StorageLimit(space), Keep: copies.Keep, Server: server}
	for _, one := range every {
		if !slices.Contains(props.Servers, one.Server) {
			props.Servers = append(props.Servers, one.Server)
		}
		if server == "" || one.Server == server {
			props.Copies = append(props.Copies, one)
		}
	}
	slices.Sort(props.Servers)
	props.All = len(props.Copies)
	if !r.URL.Query().Has("all") && len(props.Copies) > copiesShown {
		props.Copies = props.Copies[:copiesShown]
		query := url.Values{"all": {""}}
		if server != "" {
			query.Set("server", server)
		}
		props.More = "/cloud/copies?" + query.Encode()
	}
	render(w, r, pages.CloudCopies(props))
}

// PlanPage is /cloud/plan: the plan, what the workspace uses of it, and
// the way to pay.
func (c *Cloud) PlanPage(w http.ResponseWriter, r *http.Request) {
	space, frame, ok := c.page(w, r)
	if !ok {
		return
	}
	servers, err := c.store.ServerNames(space.ID)
	if err != nil {
		renderError(w, "the servers", err)
		return
	}
	stored, err := c.store.Storage(space.ID)
	if err != nil {
		renderError(w, "the storage of the copies", err)
		return
	}
	render(w, r, pages.CloudPlan(pages.CloudPlanProps{Cloud: frame, Servers: len(servers), Stored: stored, Limit: copies.StorageLimit(space)}))
}

// ConnectPage is /cloud/connect: the command that connects a server.
func (c *Cloud) ConnectPage(w http.ResponseWriter, r *http.Request) {
	_, frame, ok := c.page(w, r)
	if !ok {
		return
	}
	render(w, r, pages.CloudConnect(frame))
}
