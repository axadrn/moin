package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/api"
	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/components/toast"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// AppsHandler is the apps pages of the web UI. Deploys come from `ops
// deploy`; the web UI follows them, rolls back, shows logs and keeps env vars.
type AppsHandler struct {
	apps    *service.AppService
	servers *service.ServerService
}

func NewAppsHandler(apps *service.AppService, servers *service.ServerService) *AppsHandler {
	return &AppsHandler{apps: apps, servers: servers}
}

func (h *AppsHandler) AppsPage(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	apps, err := h.apps.List(workspace.ID)
	if err != nil {
		renderError(w, "list apps failed", err)
		return
	}
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		renderError(w, "list servers failed", err)
		return
	}
	render(w, r, pages.Apps(pages.AppsView{Apps: apps, Servers: servers, Newest: h.newestDeploys(workspace.ID, apps)}))
}

// List answers the app table, for the reload after a change.
func (h *AppsHandler) List(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	apps, err := h.apps.List(workspace.ID)
	if err != nil {
		renderError(w, "list apps failed", err)
		return
	}
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		renderError(w, "list servers failed", err)
		return
	}
	render(w, r, pages.AppList(pages.AppsView{Apps: apps, Servers: servers, Newest: h.newestDeploys(workspace.ID, apps)}))
}

// newestDeploys is the newest deploy of every app that has one, with the
// service the last good deploy detected, for the list's service and time
// columns. An app it cannot read just shows less.
func (h *AppsHandler) newestDeploys(workspaceID string, apps []model.App) map[string]api.Deploy {
	newest := make(map[string]api.Deploy, len(apps))
	for _, app := range apps {
		if app.LatestNumber == 0 {
			continue
		}
		_, deploys, err := h.apps.Deploys(workspaceID, app.ID, 10)
		if err != nil || len(deploys) == 0 {
			continue
		}
		deploy := api.NewDeploy(deploys[0])
		// A deploy still running has not detected anything yet; show what runs.
		for _, earlier := range deploys[1:] {
			if deploy.Detected != nil {
				break
			}
			if earlier.Status == model.DeploySucceeded {
				deploy.Detected = api.NewDeploy(earlier).Detected
			}
		}
		newest[app.ID] = deploy
	}
	return newest
}

// Nav answers the apps in the sidebar. htmx names the page the sidebar is
// on in HX-Current-URL, so the app open there is marked, also after a
// boosted swap.
func (h *AppsHandler) Nav(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	apps, err := h.apps.List(workspace.ID)
	if err != nil {
		renderError(w, "list apps failed", err)
		return
	}
	var at string
	if current, err := url.Parse(r.Header.Get("HX-Current-URL")); err == nil {
		at = current.Path
	}
	render(w, r, pages.AppsNav(apps, at))
}

// Create adds an app and answers the reset form. The event stream reloads the table.
func (h *AppsHandler) Create(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		renderError(w, "list servers failed", err)
		return
	}
	name, server := r.FormValue("name"), r.FormValue("server")
	_, err = h.apps.Create(actor(r), name, server)
	switch {
	case errors.Is(err, service.ErrAppName):
		render(w, r, pages.AppAddForm(servers, name, server, "Use lowercase letters, digits and dashes."))
		return
	case errors.Is(err, repository.ErrDuplicateApp):
		render(w, r, pages.AppAddForm(servers, name, server, "An app with this name already exists."))
		return
	case errors.Is(err, repository.ErrServerNotFound):
		render(w, r, pages.AppAddForm(servers, name, server, "Choose a server."))
		return
	case err != nil:
		h.toast(w, r, "App could not be added", err)
		return
	}
	render(w, r, pages.AppAddForm(servers, "", "", ""))
}

func (h *AppsHandler) AppPage(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, deploys, err := h.apps.Deploys(workspace.ID, r.PathValue("app"), 20)
	if errors.Is(err, repository.ErrAppNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	env, err := h.apps.Env(workspace.ID, app.ID)
	if err != nil {
		renderError(w, "load env failed", err)
		return
	}
	render(w, r, pages.AppPage(*app, api.NewDeploys(deploys, app.LiveNumber), sortedKeys(env)))
}

// Detail answers the header and deploys of an app, for the reload after a
// change. An app removed meanwhile sends the page to the list.
func (h *AppsHandler) Detail(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, deploys, err := h.apps.Deploys(workspace.ID, r.PathValue("app"), 20)
	if errors.Is(err, repository.ErrAppNotFound) {
		w.Header().Set("HX-Redirect", ctxkeys.AppBasePath(r.Context())+"/apps")
		return
	}
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	render(w, r, pages.AppDetail(*app, api.NewDeploys(deploys, app.LiveNumber)))
}

// Rollback starts the release of an earlier deploy again. The page follows
// the new running deploy on its own.
func (h *AppsHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	number, _ := strconv.Atoi(r.FormValue("number"))
	_, err := h.apps.Rollback(actor(r), r.PathValue("app"), number)
	if err != nil {
		h.toast(w, r, "Rollback could not start", err)
		return
	}
	h.Detail(w, r)
}

// Redeploy builds the newest source again. The page follows the new deploy.
func (h *AppsHandler) Redeploy(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.Redeploy(actor(r), r.PathValue("app"))
	if err != nil {
		h.toast(w, r, "Redeploy could not start", err)
		return
	}
	h.Detail(w, r)
}

// Restart starts the live release again with the env and routing as they
// are now, without a build. The page follows the new deploy.
func (h *AppsHandler) Restart(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.Restart(actor(r), r.PathValue("app"))
	if err != nil {
		h.toast(w, r, "Restart could not start", err)
		return
	}
	h.Detail(w, r)
}

// Start runs a stopped app again. The page follows the new deploy.
func (h *AppsHandler) Start(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.Start(actor(r), r.PathValue("app"))
	if err != nil {
		h.toast(w, r, "Start failed", err)
		return
	}
	h.Detail(w, r)
}

// Stop stops the app's containers. Its data stays.
func (h *AppsHandler) Stop(w http.ResponseWriter, r *http.Request) {
	err := h.apps.Stop(r.Context(), actor(r), r.PathValue("app"))
	if err != nil {
		h.toast(w, r, "Stop failed", err)
		return
	}
	h.Detail(w, r)
}

// DeployImage deploys an image built elsewhere. The page follows the new deploy.
func (h *AppsHandler) DeployImage(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.Deploy(actor(r), r.PathValue("app"), strings.TrimSpace(r.FormValue("image")))
	if err != nil {
		h.toast(w, r, "Image deploy could not start", err)
		return
	}
	h.Detail(w, r)
}

// RotateHook shows a new deploy hook URL once.
func (h *AppsHandler) RotateHook(w http.ResponseWriter, r *http.Request) {
	url, err := h.apps.RotateDeployHook(actor(r), r.PathValue("app"))
	if err != nil {
		h.toast(w, r, "Deploy hook could not be created", err)
		return
	}
	render(w, r, pages.AppHook(r.PathValue("app"), url))
}

// SetRegistry saves a registry login from the settings form.
func (h *AppsHandler) SetRegistry(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	err := h.apps.SetRegistry(actor(r), r.FormValue("host"), r.FormValue("username"), r.FormValue("password"))
	message := ""
	if err != nil {
		message = errorMessage(err)
	}
	h.renderRegistries(w, r, workspace.ID, message)
}

// DeleteRegistry removes a registry login.
func (h *AppsHandler) DeleteRegistry(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	err := h.apps.RemoveRegistry(actor(r), r.PathValue("host"))
	message := ""
	if err != nil {
		message = errorMessage(err)
	}
	h.renderRegistries(w, r, workspace.ID, message)
}

func (h *AppsHandler) renderRegistries(w http.ResponseWriter, r *http.Request, workspaceID, formError string) {
	logins, err := h.apps.Registries(workspaceID)
	if err != nil {
		renderError(w, "list registries failed", err)
		return
	}
	render(w, r, pages.SettingsRegistriesSection(logins, formError))
}

// Logs answers the last lines of the app's containers.
func (h *AppsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	lines, err := h.apps.Logs(r.Context(), workspace.ID, r.PathValue("app"), 200)
	if err != nil {
		render(w, r, pages.AppLogs(nil, errorMessage(err)))
		return
	}
	render(w, r, pages.AppLogs(lines, ""))
}

// Env answers the env table; with show=1 the values are visible.
func (h *AppsHandler) Env(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	h.renderEnv(w, r, workspace.ID, r.URL.Query().Get("show") == "1", "")
}

// SetEnv sets one env var from the add form.
func (h *AppsHandler) SetEnv(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	key := strings.TrimSpace(r.FormValue("key"))
	_, err := h.apps.SetEnv(actor(r), r.PathValue("app"), map[string]string{key: r.FormValue("value")}, nil)
	if err != nil {
		h.renderEnv(w, r, workspace.ID, false, errorMessage(err))
		return
	}
	h.renderEnv(w, r, workspace.ID, false, "")
}

// UnsetEnv removes one env var.
func (h *AppsHandler) UnsetEnv(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	_, err := h.apps.SetEnv(actor(r), r.PathValue("app"), nil, []string{r.PathValue("key")})
	if err != nil {
		h.renderEnv(w, r, workspace.ID, false, errorMessage(err))
		return
	}
	h.renderEnv(w, r, workspace.ID, false, "")
}

func (h *AppsHandler) renderEnv(w http.ResponseWriter, r *http.Request, workspaceID string, show bool, formError string) {
	name := r.PathValue("app")
	env, err := h.apps.Env(workspaceID, name)
	if err != nil {
		renderError(w, "load env failed", err)
		return
	}
	values := map[string]string{}
	if show {
		values = env
	}
	render(w, r, pages.AppEnv(name, sortedKeys(env), values, formError))
}

// Delete stops the app on its server and removes it, then goes to the list.
func (h *AppsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.apps.Delete(r.Context(), actor(r), r.PathValue("app"))
	if err != nil {
		h.toast(w, r, "App could not be removed", err)
		return
	}
	w.Header().Set("HX-Redirect", ctxkeys.AppBasePath(r.Context())+"/apps")
}

func (h *AppsHandler) toast(w http.ResponseWriter, r *http.Request, title string, err error) {
	renderOOB(w, r, toast.Toast(toast.Props{Title: title, Description: errorMessage(err), Type: toast.TypeError}), "beforeend:#toast-container")
}

// errorMessage is the message the API would answer for err.
func errorMessage(err error) string {
	_, detail, known := api.ErrorFor(err)
	if !known {
		slog.Error("apps request failed", "error", err)
	}
	for _, message := range detail.Fields {
		return message
	}
	return detail.Message
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// Routing answers the domains and access panel with fresh DNS checks.
func (h *AppsHandler) Routing(w http.ResponseWriter, r *http.Request) {
	h.renderRouting(w, r, "", nil)
}

// AddDomain adds one domain and applies it.
func (h *AppsHandler) AddDomain(w http.ResponseWriter, r *http.Request) {
	h.changeDomains(w, r, func(domains []string, canonical string) ([]string, string) {
		return append(domains, strings.TrimSpace(r.FormValue("domain"))), canonical
	})
}

// RemoveDomain removes one domain and applies it.
func (h *AppsHandler) RemoveDomain(w http.ResponseWriter, r *http.Request) {
	domain := r.FormValue("domain")
	h.changeDomains(w, r, func(domains []string, canonical string) ([]string, string) {
		if canonical == domain {
			canonical = ""
		}
		return slices.DeleteFunc(domains, func(d string) bool { return d == domain }), canonical
	})
}

// MakeCanonical lets the other domains redirect to one.
func (h *AppsHandler) MakeCanonical(w http.ResponseWriter, r *http.Request) {
	h.changeDomains(w, r, func(domains []string, _ string) ([]string, string) {
		return domains, r.FormValue("domain")
	})
}

func (h *AppsHandler) changeDomains(w http.ResponseWriter, r *http.Request, change func([]string, string) ([]string, string)) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.apps.Get(workspace.ID, r.PathValue("app"))
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	domains, canonical := change(app.DomainList(), app.Canonical)
	deploy, changed, err := h.apps.SetDomains(actor(r), app.ID, domains, canonical)
	h.renderRouting(w, r, appliedNotice(deploy, changed), err)
}

// Protect puts a password in front of the app.
func (h *AppsHandler) Protect(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("password") == "" {
		h.renderRouting(w, r, "", service.ErrProtect)
		return
	}
	deploy, changed, err := h.apps.SetProtection(actor(r), r.PathValue("app"), r.FormValue("username"), r.FormValue("password"))
	h.renderRouting(w, r, appliedNotice(deploy, changed), err)
}

// Unprotect makes the app public again.
func (h *AppsHandler) Unprotect(w http.ResponseWriter, r *http.Request) {
	deploy, changed, err := h.apps.SetProtection(actor(r), r.PathValue("app"), "", "")
	h.renderRouting(w, r, appliedNotice(deploy, changed), err)
}

func appliedNotice(deploy *model.Deploy, changed bool) string {
	if !changed {
		return "Nothing changed."
	}
	if deploy == nil {
		return "Saved. The app takes it with its next deploy or start."
	}
	return "Saved. Deploy v" + strconv.Itoa(deploy.Number) + " applies it now."
}

func (h *AppsHandler) renderRouting(w http.ResponseWriter, r *http.Request, notice string, changeErr error) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.apps.Get(workspace.ID, r.PathValue("app"))
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	checks, err := h.apps.CheckDomains(r.Context(), workspace.ID, app.ID)
	if err != nil {
		renderError(w, "check domains failed", err)
		return
	}
	view := pages.RoutingView{App: *app, Checks: checks, Notice: notice}
	if server, err := h.servers.Get(workspace.ID, app.ServerID); err == nil {
		view.Server = *server
	}
	if changeErr != nil {
		view.Notice, view.Error = "", errorMessage(changeErr)
	}
	render(w, r, pages.AppRouting(view))
}

// actor is the member who sends the request, in the workspace of its path.
func actor(r *http.Request) service.Actor {
	return service.Actor{WorkspaceID: ctxkeys.Workspace(r.Context()).ID, UserID: ctxkeys.User(r.Context()).ID, Via: "web"}
}
