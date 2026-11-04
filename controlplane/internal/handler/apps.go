package handler

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/api"
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
	"github.com/axadrn/moin/internal/dotenv"
)

// AppsHandler is the apps pages of the web UI. Deploys come from `moin
// deploy`; the web UI follows them, rolls back, shows logs and keeps env vars.
type AppsHandler struct {
	apps    *service.AppService
	servers *service.ServerService
	users   *service.UserService
}

func NewAppsHandler(apps *service.AppService, servers *service.ServerService, users *service.UserService) *AppsHandler {
	return &AppsHandler{apps: apps, servers: servers, users: users}
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

// Create adds an app and answers the reset form. The event stream reloads the table.
func (h *AppsHandler) Create(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		renderError(w, "list servers failed", err)
		return
	}
	app, err := h.apps.Create(actor(r), r.FormValue("name"), r.FormValue("server"))
	if errors.Is(err, repository.ErrServerNotFound) {
		fieldError(w, "App not added", "app-server", "Choose a server.")
		return
	}
	if err != nil {
		failed(w, r, "App not added", err, map[string]string{"name": "app-name"})
		return
	}
	render(w, r, pages.AppAddForm(servers))
	toastDone(w, r, app.Name+" added", "Run moin deploy "+app.Name+" in its repo to ship it.")
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
	deploy, err := h.apps.Rollback(actor(r), r.PathValue("app"), number)
	if err != nil {
		failed(w, r, "Rollback not started", err, nil)
		return
	}
	h.Detail(w, r)
	toastDone(w, r, "Rollback started", fmt.Sprintf("v%d releases v%d again.", deploy.Number, number))
}

// Redeploy builds the newest source again. The page follows the new deploy.
func (h *AppsHandler) Redeploy(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.Redeploy(actor(r), r.PathValue("app"))
	if err != nil {
		failed(w, r, "Redeploy not started", err, nil)
		return
	}
	h.Detail(w, r)
	toastDone(w, r, "Redeploy started", fmt.Sprintf("v%d builds the newest source.", deploy.Number))
}

// Restart starts the live release again with the env and routing as they
// are now, without a build. The page follows the new deploy.
func (h *AppsHandler) Restart(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.Restart(actor(r), r.PathValue("app"))
	if err != nil {
		failed(w, r, "Restart not started", err, nil)
		return
	}
	h.Detail(w, r)
	toastDone(w, r, "Restart started", fmt.Sprintf("v%d starts the live release again.", deploy.Number))
}

// Start runs a stopped app again. The page follows the new deploy.
func (h *AppsHandler) Start(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.Start(actor(r), r.PathValue("app"))
	if errors.Is(err, service.ErrRunning) {
		// Started elsewhere: the page shows it running.
		h.Detail(w, r)
		toastDone(w, r, r.PathValue("app")+" is already running", "Nothing changed.")
		return
	}
	if err != nil {
		failed(w, r, "App not started", err, nil)
		return
	}
	h.Detail(w, r)
	toastDone(w, r, r.PathValue("app")+" starting", fmt.Sprintf("v%d runs it again.", deploy.Number))
}

// Stop stops the app's containers. Its data stays.
func (h *AppsHandler) Stop(w http.ResponseWriter, r *http.Request) {
	err := h.apps.Stop(r.Context(), actor(r), r.PathValue("app"))
	if err != nil {
		failed(w, r, "App not stopped", err, nil)
		return
	}
	h.Detail(w, r)
	toastDone(w, r, r.PathValue("app")+" stopped", "Its data and settings stay. Start runs it again.")
}

// DeployImage deploys an image built elsewhere. The page follows the new deploy.
func (h *AppsHandler) DeployImage(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.Deploy(actor(r), r.PathValue("app"), strings.TrimSpace(r.FormValue("image")))
	if err != nil {
		failed(w, r, "Image not deployed", err, map[string]string{"image": "image-ref"})
		return
	}
	h.Detail(w, r)
	toastDone(w, r, "Image deploy started", fmt.Sprintf("v%d pulls %s.", deploy.Number, deploy.Image))
}

// RotateHook shows a new deploy hook URL once.
func (h *AppsHandler) RotateHook(w http.ResponseWriter, r *http.Request) {
	before, err := h.apps.Get(ctxkeys.Workspace(r.Context()).ID, r.PathValue("app"))
	if err != nil {
		failed(w, r, "Deploy hook not created", err, nil)
		return
	}
	url, err := h.apps.RotateDeployHook(actor(r), before.Name)
	if err != nil {
		failed(w, r, "Deploy hook not created", err, nil)
		return
	}
	render(w, r, pages.AppHook(before.Name, url, true))
	if before.DeployHookHash == nil {
		toastDone(w, r, "Deploy hook created", "Copy the URL now: it shows only once.")
		return
	}
	toastDone(w, r, "Deploy hook created", "The old URL stopped working.")
}

// RemoveHook turns the deploy hook off; its URL stops working.
func (h *AppsHandler) RemoveHook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("app")
	_, err := h.apps.RemoveDeployHook(actor(r), name)
	if err != nil {
		failed(w, r, "Deploy hook not turned off", err, nil)
		return
	}
	render(w, r, pages.AppHook(name, "", false))
	toastDone(w, r, "Deploy hook turned off", "CI can no longer deploy "+name+".")
}

// registryFields are the fields of the registry form, by API field name.
var registryFields = map[string]string{"host": "registry-host", "password": "registry-password"}

// SetRegistry saves a registry login from the settings form.
func (h *AppsHandler) SetRegistry(w http.ResponseWriter, r *http.Request) {
	host := strings.TrimSpace(r.FormValue("host"))
	err := h.apps.SetRegistry(actor(r), host, r.FormValue("username"), r.FormValue("password"))
	if err != nil {
		failed(w, r, "Login not saved", err, registryFields)
		return
	}
	h.renderRegistries(w, r)
	toastDone(w, r, "Login for "+host+" saved", "Your servers pull private images from it now.")
}

// DeleteRegistry removes a registry login.
func (h *AppsHandler) DeleteRegistry(w http.ResponseWriter, r *http.Request) {
	err := h.apps.RemoveRegistry(actor(r), r.PathValue("host"))
	if err != nil {
		failed(w, r, "Login not removed", err, nil)
		return
	}
	h.renderRegistries(w, r)
	toastDone(w, r, "Login for "+r.PathValue("host")+" removed", "")
}

func (h *AppsHandler) renderRegistries(w http.ResponseWriter, r *http.Request) {
	logins, err := h.apps.Registries(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		renderError(w, "list registries failed", err)
		return
	}
	render(w, r, pages.SettingsRegistriesSection(logins))
}

// Logs answers the last lines of the app's containers, or why they could
// not be read: the panel is their output, an error included.
func (h *AppsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	lines, err := h.apps.Logs(r.Context(), workspace.ID, r.PathValue("app"), 200)
	if err != nil {
		render(w, r, pages.AppLogs(nil, errorMessage(err)))
		return
	}
	render(w, r, pages.AppLogs(lines, ""))
}

// Env answers the env table; with show=1 the values are visible, with
// text=1 they are one .env text to edit.
func (h *AppsHandler) Env(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("text") == "1" {
		name := r.PathValue("app")
		env, err := h.apps.Env(ctxkeys.Workspace(r.Context()).ID, name)
		if err != nil {
			renderError(w, "load env failed", err)
			return
		}
		render(w, r, pages.AppEnvText(name, dotenv.Format(env), ""))
		return
	}
	h.renderEnv(w, r, r.URL.Query().Get("show") == "1")
}

// EnvText takes the env vars as one .env text: first it shows what changes,
// with confirm=1 it replaces them, with back=1 it goes back to the text.
func (h *AppsHandler) EnvText(w http.ResponseWriter, r *http.Request) {
	name, text := r.PathValue("app"), r.FormValue("text")
	next, err := dotenv.Parse(text)
	if err != nil || r.FormValue("back") == "1" {
		problem := ""
		if err != nil {
			problem = strings.ToUpper(err.Error()[:1]) + err.Error()[1:] + ". Nothing changed."
		}
		render(w, r, pages.AppEnvText(name, text, problem))
		return
	}
	if r.FormValue("confirm") != "1" {
		current, err := h.apps.Env(ctxkeys.Workspace(r.Context()).ID, name)
		if err != nil {
			renderError(w, "load env failed", err)
			return
		}
		change := dotenv.Diff(current, next)
		if change.None() {
			h.renderEnv(w, r, false)
			toastDone(w, r, "Nothing changed", "The text has the env vars there are.")
			return
		}
		render(w, r, pages.AppEnvReview(name, text, change))
		return
	}
	change, err := h.apps.ReplaceEnv(actor(r), name, next)
	if err != nil {
		render(w, r, pages.AppEnvText(name, text, errorMessage(err)+" Nothing changed."))
		return
	}
	h.renderEnv(w, r, false)
	toastDone(w, r, "Env vars replaced", "It "+change.String()+". The next deploy or restart hands them to the app.")
}

// SetEnv sets one env var from the add form.
func (h *AppsHandler) SetEnv(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.FormValue("key"))
	_, err := h.apps.SetEnv(actor(r), r.PathValue("app"), map[string]string{key: r.FormValue("value")}, nil)
	if errors.Is(err, service.ErrEnvValue) {
		fieldError(w, key+" not saved", "env-value", errorMessage(err))
		return
	}
	if err != nil {
		failed(w, r, "Variable not saved", err, map[string]string{"set": "env-key"})
		return
	}
	h.renderEnv(w, r, false)
	toastDone(w, r, key+" saved", "The next deploy or restart hands it to the app.")
}

// UnsetEnv removes one env var.
func (h *AppsHandler) UnsetEnv(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	_, err := h.apps.SetEnv(actor(r), r.PathValue("app"), nil, []string{key})
	if err != nil {
		failed(w, r, key+" not removed", err, nil)
		return
	}
	h.renderEnv(w, r, false)
	toastDone(w, r, key+" removed", "The next deploy or restart drops it.")
}

func (h *AppsHandler) renderEnv(w http.ResponseWriter, r *http.Request, show bool) {
	name := r.PathValue("app")
	env, err := h.apps.Env(ctxkeys.Workspace(r.Context()).ID, name)
	if err != nil {
		renderError(w, "load env failed", err)
		return
	}
	values := map[string]string{}
	if show {
		values = env
	}
	render(w, r, pages.AppEnv(name, sortedKeys(env), values))
}

// Delete stops the app on its server and removes it, then goes to the list.
func (h *AppsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.apps.Delete(r.Context(), actor(r), r.PathValue("app"))
	if err != nil {
		failed(w, r, "App not removed", err, nil)
		return
	}
	w.Header().Set("HX-Redirect", ctxkeys.AppBasePath(r.Context())+"/apps")
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
	h.renderRouting(w, r)
}

// AddDomain adds one domain, to the main service or the one named, and
// applies it.
func (h *AppsHandler) AddDomain(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.FormValue("domain"))
	if service := strings.TrimSpace(r.FormValue("service")); service != "" {
		h.changeServiceDomains(w, r, service, domain+" added", "Domain not added", func(domains []string) []string {
			return append(domains, domain)
		})
		return
	}
	h.changeDomains(w, r, domain+" added", "Domain not added", func(domains []string, canonical string) ([]string, string) {
		return append(domains, domain), canonical
	})
}

// RemoveDomain removes one domain and applies it.
func (h *AppsHandler) RemoveDomain(w http.ResponseWriter, r *http.Request) {
	domain := r.FormValue("domain")
	if service := r.FormValue("service"); service != "" {
		h.changeServiceDomains(w, r, service, domain+" removed", domain+" not removed", func(domains []string) []string {
			return slices.DeleteFunc(domains, func(d string) bool { return d == domain })
		})
		return
	}
	h.changeDomains(w, r, domain+" removed", domain+" not removed", func(domains []string, canonical string) ([]string, string) {
		if canonical == domain {
			canonical = ""
		}
		return slices.DeleteFunc(domains, func(d string) bool { return d == domain }), canonical
	})
}

// MakeCanonical lets the other domains redirect to one.
func (h *AppsHandler) MakeCanonical(w http.ResponseWriter, r *http.Request) {
	domain := r.FormValue("domain")
	h.changeDomains(w, r, domain+" is canonical", "Canonical domain not changed", func(domains []string, _ string) ([]string, string) {
		return domains, domain
	})
}

func (h *AppsHandler) changeDomains(w http.ResponseWriter, r *http.Request, done, notDone string, change func([]string, string) ([]string, string)) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.apps.Get(workspace.ID, r.PathValue("app"))
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	domains, canonical := change(app.DomainList(), app.Canonical)
	deploy, changed, err := h.apps.SetDomains(actor(r), app.ID, domains, canonical)
	if err != nil {
		failed(w, r, notDone, err, map[string]string{"domains": "domain-name"})
		return
	}
	h.routingChanged(w, r, done, deploy, changed)
}

func (h *AppsHandler) changeServiceDomains(w http.ResponseWriter, r *http.Request, service, done, notDone string, change func([]string) []string) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.apps.Get(workspace.ID, r.PathValue("app"))
	if err != nil {
		renderError(w, "load app failed", err)
		return
	}
	domains := change(slices.Clone(app.ServiceDomainMap()[service]))
	deploy, changed, err := h.apps.SetServiceDomains(actor(r), app.ID, service, domains)
	if err != nil {
		failed(w, r, notDone, err, map[string]string{"domains": "domain-name", "service": "domain-service"})
		return
	}
	h.routingChanged(w, r, done, deploy, changed)
}

// SetInstant picks the provider of the instant address, or off.
func (h *AppsHandler) SetInstant(w http.ResponseWriter, r *http.Request) {
	provider := r.FormValue("provider")
	deploy, changed, err := h.apps.SetInstant(actor(r), r.PathValue("app"), provider, nil)
	if err != nil {
		failed(w, r, "Instant address not changed", err, nil)
		return
	}
	done := "Instant address via " + provider
	if provider == "off" {
		done = "Instant address off"
	}
	h.routingChanged(w, r, done, deploy, changed)
}

// SetInstantPassword puts the password in front of the instant address, or
// takes it away.
func (h *AppsHandler) SetInstantPassword(w http.ResponseWriter, r *http.Request) {
	open := r.FormValue("on") != "1"
	deploy, changed, err := h.apps.SetInstant(actor(r), r.PathValue("app"), "", &open)
	if err != nil {
		failed(w, r, "Password not changed", err, nil)
		return
	}
	done := "Password on the instant address"
	if open {
		done = "Instant address open to everyone"
	}
	h.routingChanged(w, r, done, deploy, changed)
}

// Protect puts a password in front of the app. A wrong field says the
// rule of its hint line again, in red.
func (h *AppsHandler) Protect(w http.ResponseWriter, r *http.Request) {
	var deploy *model.Deploy
	changed, err := false, service.ErrProtect
	if r.FormValue("password") != "" {
		deploy, changed, err = h.apps.SetProtection(actor(r), r.PathValue("app"), r.FormValue("username"), r.FormValue("password"))
	}
	switch {
	case errors.Is(err, service.ErrProtectUser):
		fieldError(w, "Password not set", "protect-user", "No spaces or colons.")
		return
	case errors.Is(err, service.ErrProtect):
		fieldError(w, "Password not set", "protect-password", "Use 8 to 72 characters.")
		return
	case err != nil:
		failed(w, r, "Password not set", err, nil)
		return
	}
	h.routingChanged(w, r, "Password set", deploy, changed)
}

// Unprotect makes the app public again.
func (h *AppsHandler) Unprotect(w http.ResponseWriter, r *http.Request) {
	deploy, changed, err := h.apps.SetProtection(actor(r), r.PathValue("app"), "", "")
	if err != nil {
		failed(w, r, "Password not removed", err, nil)
		return
	}
	h.routingChanged(w, r, "Password removed", deploy, changed)
}

// routingChanged answers a saved change of the domains or the password: the
// panel as it is now and a toast with what applies it.
func (h *AppsHandler) routingChanged(w http.ResponseWriter, r *http.Request, done string, deploy *model.Deploy, changed bool) {
	h.renderRouting(w, r)
	switch {
	case !changed:
		toastDone(w, r, "Nothing changed", "It was like this already.")
	case deploy == nil:
		toastDone(w, r, done, "The app takes it with its next deploy or start.")
	default:
		toastDone(w, r, done, "Deploy v"+strconv.Itoa(deploy.Number)+" applies it now.")
	}
}

func (h *AppsHandler) renderRouting(w http.ResponseWriter, r *http.Request) {
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
	instant, err := h.apps.Instant(workspace.ID, app.ID, r.URL.Query().Get("show") == "true")
	if err != nil {
		renderError(w, "load instant address failed", err)
		return
	}
	view := pages.RoutingView{App: *app, Checks: checks, Instant: instant}
	if server, err := h.servers.Get(workspace.ID, app.ServerID); err == nil {
		view.Server = *server
	}
	render(w, r, pages.AppRouting(view))
}

// actor is the member who sends the request, in the workspace of its path.
func actor(r *http.Request) service.Actor {
	return service.Actor{WorkspaceID: ctxkeys.Workspace(r.Context()).ID, UserID: ctxkeys.User(r.Context()).ID, Source: service.RequestSource(r.Context())}
}
