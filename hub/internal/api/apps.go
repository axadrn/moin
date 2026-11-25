package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/internal/dotenv"
	"github.com/axadrn/moin/internal/link"
	"github.com/axadrn/moin/protocol"
)

// AppsHandler serves apps, their deploys, logs and env. The web UI, the CLI
// and the TUI all use it.
type AppsHandler struct {
	apps *service.AppService
}

func NewAppsHandler(apps *service.AppService) *AppsHandler {
	return &AppsHandler{apps: apps}
}

// App is the public JSON shape of an app.
type App struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ServerID     string   `json:"server_id"`
	ServerName   string   `json:"server_name"`
	LatestNumber int      `json:"latest_number"`
	LatestStatus string   `json:"latest_status"`
	LiveNumber   int      `json:"live_number"`
	Domains      []string `json:"domains"`
	Canonical    string   `json:"canonical"`
	// Password says the app's password is on: its domains and its
	// instant address ask for it. The password itself is never here.
	Password bool `json:"password"`
	// ServiceDomains are the domains of the app's other services.
	ServiceDomains map[string][]string `json:"service_domains"`
	// Instant is the app's instant address, "" when it has none;
	// InstantProvider its provider or off.
	Instant         string `json:"instant"`
	InstantProvider string `json:"instant_provider"`
	// Stopped is set when the app was stopped; a deploy, restart or start
	// runs it again.
	Stopped bool `json:"stopped"`
	// ChangesWaiting counts the changes saved since the live deploy
	// started (env vars, domains, password, instant address, bucket); a
	// restart applies them. Only GET /apps/{app} fills it.
	ChangesWaiting int `json:"changes_waiting"`
	// Repo and Branch are the GitHub repo the app follows, if any.
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	AutoDeploy bool   `json:"auto_deploy"`
	// Pinned keeps the app above the others that run fine, in every list.
	Pinned bool `json:"pinned"`
	// Bucket is the bucket the app backs up to; empty uses the
	// workspace's default bucket.
	Bucket string `json:"bucket"`
	// Hook says the app has a deploy hook URL; the URL itself is shown
	// only once, when it is made.
	Hook bool `json:"hook"`
	// DeployFrom is where new versions come from, one at a time: ci (the
	// deploy hook, with the compose file from the linked repo when there
	// is one) or github (the linked repo); empty while neither is set up.
	// Making a hook or turning deploy on push on switches it.
	DeployFrom string `json:"deploy_from"`
	// HookImage is the image the deploy hook deploys new tags of: the one
	// running now, without its tag. Empty while the app runs from its
	// repo; the hook then deploys nothing until an image is deployed by
	// hand.
	HookImage string `json:"hook_image"`
	// Catalog is the slug of the catalog entry the app was installed
	// from; empty for an app of your own.
	Catalog   string    `json:"catalog"`
	CreatedAt time.Time `json:"created_at"`
}

// AppList is the answer of GET /api/v1/apps and every event of GET
// /api/v1/apps/events.
type AppList struct {
	Items []App `json:"items"`
}

// Deploy is the public JSON shape of a deploy. Log is only set on the
// single-deploy answer.
type Deploy struct {
	Number     int    `json:"number"`
	Status     string `json:"status"`
	FromNumber int    `json:"from_number"`
	Image      string `json:"image"`
	// Ref and Commit name the GitHub commit the deploy came from.
	Ref           string             `json:"ref,omitempty"`
	Commit        string             `json:"commit,omitempty"`
	CommitMessage string             `json:"commit_message,omitempty"`
	Error         string             `json:"error"`
	Detected      *protocol.Detected `json:"detected"`
	CreatedAt     time.Time          `json:"created_at"`
	FinishedAt    *time.Time         `json:"finished_at"`
	Log           []string           `json:"log,omitempty"`
	// OnServer says whether the server still keeps the release, so it can
	// be rolled back to. Only in the deploys of an app's detail.
	OnServer *bool `json:"on_server,omitempty"`
}

// AppDetail is the answer of GET /api/v1/apps/{app}: the app and its newest deploys.
type AppDetail struct {
	App     App      `json:"app"`
	Deploys []Deploy `json:"deploys"`
}

// Env is the answer of GET /api/v1/apps/{app}/env: the keys and when they
// changed. Values are write-only: no answer ever holds one.
type Env struct {
	Items []EnvVar `json:"items"`
}

// EnvVar is one env var of an app, without its value.
type EnvVar struct {
	Key       string    `json:"key"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewEnv is the answer for the env vars of an app.
func NewEnv(vars []model.EnvVar) Env {
	items := make([]EnvVar, len(vars))
	for index, envVar := range vars {
		items[index] = EnvVar{Key: envVar.Key, UpdatedAt: envVar.UpdatedAt.UTC()}
	}
	return Env{Items: items}
}

// Logs is the answer of GET /api/v1/apps/{app}/logs.
type Logs struct {
	Lines []string `json:"lines"`
}

type createAppRequest struct {
	Name   string `json:"name"`
	Server string `json:"server"`
}

// deployRequest is the body of a deploy: an image built elsewhere, a ref
// of the GitHub repo the app follows, or both: the image with the compose
// file of the ref. An empty body deploys the linked branch.
type deployRequest struct {
	Image string `json:"image"`
	Ref   string `json:"ref"`
}

type rollbackRequest struct {
	Number int `json:"number"`
}

type envRequest struct {
	Set   map[string]string `json:"set"`
	Unset []string          `json:"unset"`
}

func NewApp(app model.App) App {
	domains, canonical := app.DomainsOf("")
	return App{
		ID:              link.ID(app.ID),
		Name:            app.Name,
		ServerID:        link.ID(app.ServerID),
		ServerName:      app.ServerName,
		LatestNumber:    app.LatestNumber,
		LatestStatus:    app.LatestStatus,
		LiveNumber:      app.LiveNumber,
		Domains:         domains,
		Canonical:       canonical,
		Password:        app.Password != "",
		ServiceDomains:  app.ServiceDomains(),
		Instant:         instantURL(app.InstantHost),
		InstantProvider: app.Instant,
		Stopped:         app.Stopped,
		ChangesWaiting:  app.ChangesWaiting,
		Repo:            app.Repo,
		Branch:          app.Branch,
		AutoDeploy:      app.AutoDeploy,
		Pinned:          app.Pinned,
		Hook:            app.DeployHookHash != nil,
		DeployFrom:      app.DeployFrom(),
		HookImage:       app.HookImage(),
		Bucket:          app.BucketName,
		Catalog:         app.Catalog,
		CreatedAt:       app.CreatedAt.UTC(),
	}
}

func instantURL(host string) string {
	if host == "" {
		return ""
	}
	return "https://" + host
}

// Instant is the instant address of an app: its URL, or why it has none.
// The app's password guards it like the app's domains.
type Instant struct {
	URL      string `json:"url"`
	Provider string `json:"provider"`
	Reason   string `json:"reason,omitempty"`
}

func NewInstant(instant service.Instant) Instant {
	return Instant{URL: instant.URL, Provider: instant.Provider, Reason: instant.Reason}
}

// CreatedApp is the answer of POST /api/v1/apps: the app and its
// password, user moin, which its domains and its instant address ask
// for: shown this once.
type CreatedApp struct {
	App      App    `json:"app"`
	Password string `json:"password,omitempty"`
}

// NewPassword is the answer of PUT /api/v1/apps/{app}/password: the
// change, and the password when moin made it, the one time moin shows it.
type NewPassword struct {
	Change
	Password string `json:"password,omitempty"`
}

func NewDeploy(deploy model.Deploy) Deploy {
	return Deploy{
		Number:        deploy.Number,
		Status:        deploy.Status,
		FromNumber:    deploy.FromNumber,
		Image:         deploy.Image,
		Ref:           deploy.Ref,
		Commit:        deploy.CommitSHA,
		CommitMessage: deploy.CommitMessage,
		Error:         deploy.Error,
		Detected:      service.Detected(deploy),
		CreatedAt:     deploy.CreatedAt.UTC(),
		FinishedAt:    utcTime(deploy.FinishedAt),
	}
}

// NewDeploys converts the newest deploys of an app with live deploy live,
// newest first, and says which ones the server still keeps.
func NewDeploys(deploys []model.Deploy, live int) []Deploy {
	kept := service.KeptDeploys(deploys, live)
	out := make([]Deploy, 0, len(deploys))
	for _, deploy := range deploys {
		converted := NewDeploy(deploy)
		onServer := kept[deploy.Number]
		converted.OnServer = &onServer
		out = append(out, converted)
	}
	return out
}

func (h *AppsHandler) list(workspaceID string) (AppList, error) {
	apps, err := h.apps.ListSorted(workspaceID)
	if err != nil {
		return AppList{}, err
	}
	items := make([]App, 0, len(apps))
	for _, app := range apps {
		items = append(items, NewApp(app))
	}
	return AppList{Items: items}, nil
}

// List returns the apps in the order every client shows: what needs you,
// what deploys, pinned apps, then the rest by name.
func (h *AppsHandler) List(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	list, err := h.list(workspace.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, list)
}

// Create adds an app on a server.
func (h *AppsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request createAppRequest
	if !Decode(w, r, &request) {
		return
	}
	app, err := h.apps.Create(actor(r), request.Name, request.Server)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusCreated, CreatedApp{App: NewApp(*app), Password: app.NewPassword})
}

// Get returns an app with its newest deploys.
func (h *AppsHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, deploys, err := h.apps.Deploys(workspace.ID, r.PathValue("app"), 20)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, AppDetail{App: NewApp(*app), Deploys: NewDeploys(deploys, app.LiveNumber)})
}

// renameAppRequest is the body of PATCH /api/v1/apps/{app}.
type renameAppRequest struct {
	Name string `json:"name"`
}

// Rename gives an app a new name and answers it. Links name the app by its
// name, so they follow it; its data, backups and instant address stay, and
// the next restart names its containers after it.
func (h *AppsHandler) Rename(w http.ResponseWriter, r *http.Request) {
	var request renameAppRequest
	if !Decode(w, r, &request) {
		return
	}
	app, err := h.apps.Rename(actor(r), r.PathValue("app"), request.Name)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewApp(*app))
}

// Delete stops the app on its server and removes it. Its data stays there.
// ?confirm= is its name, typed out.
func (h *AppsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.apps.Delete(r.Context(), actor(r), r.PathValue("app"), r.URL.Query().Get("confirm"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}

// CreateDeploy starts a deploy: {"image": "..."} deploys an image built
// elsewhere, with the compose file of "ref" in the linked repo, its branch
// by default; {"ref": "..."} alone a branch, tag or commit of the linked
// repo, built on the server.
func (h *AppsHandler) CreateDeploy(w http.ResponseWriter, r *http.Request) {
	var request deployRequest
	if !Decode(w, r, &request) {
		return
	}
	var deploy *model.Deploy
	var err error
	switch {
	case request.Image != "":
		deploy, err = h.apps.Deploy(r.Context(), actor(r), r.PathValue("app"), request.Image, request.Ref)
	default:
		deploy, err = h.apps.DeployRef(r.Context(), actor(r), r.PathValue("app"), request.Ref)
	}
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, NewDeploy(*deploy))
}

// Redeploy builds the newest source again with the env as it is now.
func (h *AppsHandler) Redeploy(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.Redeploy(actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, NewDeploy(*deploy))
}

// Rollback starts an earlier successful release again.
func (h *AppsHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	var request rollbackRequest
	if !Decode(w, r, &request) {
		return
	}
	deploy, err := h.apps.Rollback(actor(r), r.PathValue("app"), request.Number)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, NewDeploy(*deploy))
}

// GetDeploy returns one deploy with its log.
func (h *AppsHandler) GetDeploy(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	number, ok := deployNumber(w, r)
	if !ok {
		return
	}
	deploy, lines, _, stop, err := h.apps.Follow(workspace.ID, r.PathValue("app"), number)
	if err != nil {
		ServiceError(w, err)
		return
	}
	stop()
	body := NewDeploy(*deploy)
	body.Log = lines()
	JSON(w, http.StatusOK, body)
}

// DeployEvents streams a deploy: a line event per log line, then one done
// event with the deploy. A finished deploy streams its whole log at once.
func (h *AppsHandler) DeployEvents(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	number, ok := deployNumber(w, r)
	if !ok {
		return
	}
	appName := r.PathValue("app")
	deploy, lines, changes, stop, err := h.apps.Follow(workspace.ID, appName, number)
	if err != nil {
		ServiceError(w, err)
		return
	}
	defer stop()
	events := startEvents(w)
	sent := 0
	flushLines := func() bool {
		all := lines()
		for _, line := range all[sent:] {
			if !events.send("line", map[string]string{"line": line}) {
				return false
			}
		}
		sent = len(all)
		return true
	}
	if !flushLines() {
		return
	}
	heartbeat := time.NewTicker(serverEventsHeartbeat)
	defer heartbeat.Stop()
	for changes != nil {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-changes:
			if !flushLines() {
				return
			}
			if !open {
				changes = nil
			}
		case <-heartbeat.C:
			if !events.comment("heartbeat") {
				return
			}
		}
	}
	finished, err := h.apps.DeployByNumber(workspace.ID, appName, deploy.Number)
	if err != nil {
		return
	}
	events.send("done", NewDeploy(*finished))
}

// Logs returns the last lines of the app's containers from its server.
func (h *AppsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	tail := 200
	if raw := r.URL.Query().Get("tail"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			ValidationError(w, map[string]string{"tail": "Choose between 1 and 1000 lines."})
			return
		}
		tail = parsed
	}
	lines, err := h.apps.Logs(r.Context(), workspace.ID, r.PathValue("app"), tail)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, Logs{Lines: lines})
}

// ComposeFile is the compose file of an app's live deploy, YAML: everything
// moin runs for the app, without a secret value.
type ComposeFile struct {
	Number int    `json:"number"`
	Text   string `json:"compose_file"`
}

// ComposeFile answers the compose file of the app's live deploy.
func (h *AppsHandler) ComposeFile(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	number, text, err := h.apps.ComposeFile(workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, ComposeFile{Number: number, Text: text})
}

// GetEnv lists the env vars by key, without their values.
func (h *AppsHandler) GetEnv(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	vars, err := h.apps.EnvVars(workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewEnv(vars))
}

// UpdateEnv sets and removes env vars. The next deploy gets them.
func (h *AppsHandler) UpdateEnv(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	var request envRequest
	if !Decode(w, r, &request) {
		return
	}
	_, err := h.apps.SetEnv(r.Context(), actor(r), r.PathValue("app"), request.Set, request.Unset)
	if err != nil {
		ServiceError(w, err)
		return
	}
	vars, err := h.apps.EnvVars(workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewEnv(vars))
}

// replaceEnvRequest is every env var of an app, by key.
type replaceEnvRequest struct {
	Env map[string]string `json:"env"`
}

// ReplacedEnv is the env vars after a replace and what changed, by key.
type ReplacedEnv struct {
	Items []EnvVar `json:"items"`
	dotenv.Change
}

// ReplaceEnv makes the sent env vars the app's, all at once: keys not sent
// are removed. The next deploy gets them.
func (h *AppsHandler) ReplaceEnv(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	var request replaceEnvRequest
	if !Decode(w, r, &request) {
		return
	}
	if request.Env == nil {
		request.Env = map[string]string{}
	}
	change, err := h.apps.ReplaceEnv(r.Context(), actor(r), r.PathValue("app"), request.Env)
	if err != nil {
		ServiceError(w, err)
		return
	}
	vars, err := h.apps.EnvVars(workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, ReplacedEnv{Items: NewEnv(vars).Items, Change: change})
}

// Events streams the app list: once on connect and again after every change.
func (h *AppsHandler) Events(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	changes, unsubscribe := h.apps.Subscribe(workspace.ID)
	defer unsubscribe()
	events := startEvents(w)
	send := func() bool {
		list, err := h.list(workspace.ID)
		if err != nil {
			slog.Error("failed to list apps", "error", err, "workspace_id", workspace.ID)
			return false
		}
		return events.send("apps", list)
	}
	if !send() {
		return
	}
	heartbeat := time.NewTicker(serverEventsHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			if !send() {
				return
			}
		case <-heartbeat.C:
			if !events.comment("heartbeat") {
				return
			}
		}
	}
}

func deployNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number < 1 {
		Error(w, http.StatusNotFound, "not_found", "The deploy was not found.")
		return 0, false
	}
	return number, true
}

// eventStream writes server-sent events.
type eventStream struct {
	w          http.ResponseWriter
	controller *http.ResponseController
}

func startEvents(w http.ResponseWriter) eventStream {
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return eventStream{w: w, controller: controller}
}

func (e eventStream) send(event string, value any) bool {
	body, err := json.Marshal(value)
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", event, body)
	return err == nil && e.controller.Flush() == nil
}

func (e eventStream) comment(text string) bool {
	_, err := fmt.Fprintf(e.w, ": %s\n\n", text)
	return err == nil && e.controller.Flush() == nil
}

// DeployHook is the answer of POST /api/v1/apps/{app}/hook. The URL is shown once.
type DeployHook struct {
	URL string `json:"url"`
}

// RotateHook gives the app a new deploy hook URL; the old one stops working.
func (h *AppsHandler) RotateHook(w http.ResponseWriter, r *http.Request) {
	url, err := h.apps.RotateDeployHook(actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, DeployHook{URL: url})
}

// RemoveHook turns the app's deploy hook off; its URL stops working.
func (h *AppsHandler) RemoveHook(w http.ResponseWriter, r *http.Request) {
	_, err := h.apps.RemoveDeployHook(actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}

// Registry is a registry login without its password, which only the
// servers keep: Servers are the ones that keep it, MissingOn the others.
type Registry struct {
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	SecretServers
}

// NewRegistry is the JSON shape of a registry login.
func NewRegistry(login service.RegistryLogin) Registry {
	return Registry{Host: login.Host, Username: login.Username, CreatedAt: login.CreatedAt.UTC(), SecretServers: NewSecretServers(login.Servers)}
}

// SecretOn is a server that keeps a secret, and since when.
type SecretOn struct {
	Server    string    `json:"server"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SecretServers says which servers keep a secret and which do not.
type SecretServers struct {
	Servers   []SecretOn `json:"servers"`
	MissingOn []string   `json:"missing_on"`
}

// NewSecretServers is the JSON shape of where a secret is.
func NewSecretServers(where service.SecretServers) SecretServers {
	out := SecretServers{Servers: make([]SecretOn, 0, len(where.On)), MissingOn: where.MissingOn}
	if out.MissingOn == nil {
		out.MissingOn = []string{}
	}
	for _, on := range where.On {
		out.Servers = append(out.Servers, SecretOn{Server: on.Server, UpdatedAt: on.UpdatedAt.UTC()})
	}
	return out
}

// RegistryList is the answer of GET /api/v1/registries.
type RegistryList struct {
	Items []Registry `json:"items"`
}

// registryRequest is a login for one server: server may be empty when
// the workspace has one.
type registryRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Server   string `json:"server"`
}

func (h *AppsHandler) registryList(workspaceID string) (RegistryList, error) {
	logins, err := h.apps.Registries(workspaceID)
	if err != nil {
		return RegistryList{}, err
	}
	list := RegistryList{Items: make([]Registry, 0, len(logins))}
	for _, login := range logins {
		list.Items = append(list.Items, NewRegistry(login))
	}
	return list, nil
}

// Registries lists the registry logins daemons use to pull private images.
func (h *AppsHandler) Registries(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	list, err := h.registryList(workspace.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, list)
}

// SetRegistry hands the login for one registry host to a server, which
// keeps it; the hub keeps the host and the username.
func (h *AppsHandler) SetRegistry(w http.ResponseWriter, r *http.Request) {
	var request registryRequest
	if !Decode(w, r, &request) {
		return
	}
	err := h.apps.SetRegistry(r.Context(), actor(r), r.PathValue("host"), request.Username, request.Password, request.Server)
	if err != nil {
		ServiceError(w, err)
		return
	}
	h.Registries(w, r)
}

// DeleteRegistry removes the login for one registry host.
func (h *AppsHandler) DeleteRegistry(w http.ResponseWriter, r *http.Request) {
	err := h.apps.RemoveRegistry(r.Context(), actor(r), r.PathValue("host"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}

// Change is the answer to a change of the domains: the app as it is now.
// The app takes it with its next restart, start or deploy. Unchanged is set
// when the app had it so already.
type Change struct {
	App       App  `json:"app"`
	Unchanged bool `json:"unchanged,omitempty"`
}

type domainsRequest struct {
	Domains   []string `json:"domains"`
	Canonical string   `json:"canonical"`
	// Service sets the domains of another service than the main one.
	Service string `json:"service"`
}

type instantRequest struct {
	Provider string `json:"provider"`
}

type passwordRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// DomainCheck says whether a domain points at the app's server.
type DomainCheck struct {
	Domain    string `json:"domain"`
	Canonical bool   `json:"canonical"`
	Address   string `json:"address"`
	// IPv4 and IPv6 are the server's addresses, empty when unknown.
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
	// Advice is the DNS record to set, such as "A record to 203.0.113.7".
	Advice string   `json:"advice"`
	Found  []string `json:"found"`
	Points bool     `json:"points"`
	// Service is the service of compose.yml the domain reaches, empty for
	// the main one.
	Service string `json:"service,omitempty"`
}

// DomainChecks is the answer of GET /api/v1/apps/{app}/domains.
type DomainChecks struct {
	Items []DomainCheck `json:"items"`
}

func NewDomainChecks(checks []service.DomainCheck) DomainChecks {
	out := DomainChecks{Items: make([]DomainCheck, 0, len(checks))}
	for _, check := range checks {
		found := check.Found
		if found == nil {
			found = []string{}
		}
		out.Items = append(out.Items, DomainCheck{Domain: check.Domain, Canonical: check.Canonical, Address: check.Address, IPv4: check.IPv4, IPv6: check.IPv6, Advice: check.Advice, Found: found, Points: check.Points, Service: check.Service})
	}
	return out
}

// NewChange is the answer to a change of app: the app as it is now.
func NewChange(apps *service.AppService, workspaceID, app string, changed bool) (Change, error) {
	current, err := apps.Get(workspaceID, app)
	if err != nil {
		return Change{}, err
	}
	return Change{App: NewApp(*current), Unchanged: !changed}, nil
}

func (h *AppsHandler) change(w http.ResponseWriter, workspaceID, app string, changed bool) {
	answer, err := NewChange(h.apps, workspaceID, app, changed)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, answer)
}

// Domains checks the DNS of every domain of the app.
func (h *AppsHandler) Domains(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	checks, err := h.apps.CheckDomains(r.Context(), workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewDomainChecks(checks))
}

// SetDomains replaces the domains and applies them to the live deploy.
func (h *AppsHandler) SetDomains(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	var request domainsRequest
	if !Decode(w, r, &request) {
		return
	}
	changed, err := h.apps.SetDomains(actor(r), r.PathValue("app"), request.Service, request.Domains, request.Canonical)
	if err != nil {
		ServiceError(w, err)
		return
	}
	h.change(w, workspace.ID, r.PathValue("app"), changed)
}

// GetInstant answers the app's instant address, without the password.
func (h *AppsHandler) GetInstant(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	instant, err := h.apps.Instant(workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewInstant(instant))
}

// SetInstant picks the provider of the app's instant address, or off.
func (h *AppsHandler) SetInstant(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	var request instantRequest
	if !Decode(w, r, &request) {
		return
	}
	changed, err := h.apps.SetInstant(actor(r), r.PathValue("app"), request.Provider)
	if err != nil {
		ServiceError(w, err)
		return
	}
	h.change(w, workspace.ID, r.PathValue("app"), changed)
}

// SetPassword turns the app's password on for its domains and its
// instant address.
// Without a password moin makes one and answers it, the one time it shows
// it.
func (h *AppsHandler) SetPassword(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	var request passwordRequest
	if !Decode(w, r, &request) {
		return
	}
	made, changed, err := h.apps.SetPassword(actor(r), r.PathValue("app"), request.Username, request.Password)
	if err != nil {
		ServiceError(w, err)
		return
	}
	answer, err := NewChange(h.apps, workspace.ID, r.PathValue("app"), changed)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewPassword{Change: answer, Password: made})
}

// DeletePassword turns the app's password off: no domain asks for one.
func (h *AppsHandler) DeletePassword(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	changed, err := h.apps.RemovePassword(actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	h.change(w, workspace.ID, r.PathValue("app"), changed)
}

// Restart starts the live deploy again with the env and domains as they
// are now.
func (h *AppsHandler) Restart(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.Restart(actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, NewDeploy(*deploy))
}

// Start runs a stopped app again.
func (h *AppsHandler) Start(w http.ResponseWriter, r *http.Request) {
	deploy, err := h.apps.Start(actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, NewDeploy(*deploy))
}

// Stop stops the app's containers. Its data stays.
func (h *AppsHandler) Stop(w http.ResponseWriter, r *http.Request) {
	err := h.apps.Stop(r.Context(), actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}

// actor is who sends the request.
func actor(r *http.Request) service.Actor {
	return service.Actor{WorkspaceID: ctxkeys.Workspace(r.Context()).ID, UserID: ctxkeys.User(r.Context()).ID, Source: service.RequestSource(r.Context())}
}

// source is how a request came in: the web, the CLI, the TUI or the API.
func source(r *http.Request) service.Source { return service.RequestSource(r.Context()) }

// SetPinned pins or unpins an app: {"pinned": true}.
func (h *AppsHandler) SetPinned(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pinned *bool `json:"pinned"`
	}
	if !Decode(w, r, &body) {
		return
	}
	if body.Pinned == nil {
		ValidationError(w, map[string]string{"pinned": "Say true or false."})
		return
	}
	app, err := h.apps.SetPinned(actor(r), r.PathValue("app"), *body.Pinned)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewApp(*app))
}
