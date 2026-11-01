package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/secret"
	"github.com/axadrn/ops/protocol"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	// deployTimeout ends a deploy the agent never reported on.
	deployTimeout     = 30 * time.Minute
	maxDeployLogLines = 5000
	maxEnvVars        = 200
	maxEnvValueSize   = 32 << 10
)

var (
	ErrAppName       = errors.New("app name must be 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit")
	ErrDeployRunning = errors.New("a deploy of this app is still running")
	ErrNoRollback    = errors.New("there is no earlier successful deploy to roll back to")
	ErrNoSource      = errors.New("no deploy has a source yet")
	ErrImage         = errors.New("image must be a reference like ghcr.io/me/app:tag")
	ErrDeployHook    = errors.New("deploy hook is invalid")
	ErrRegistryHost  = errors.New("registry host must be a host name like ghcr.io")
	ErrRegistryLogin = errors.New("registry username and password are required")
	ErrDomain        = errors.New("domains must be host names like blog.de")
	ErrCanonical     = errors.New("the canonical domain must be one of the domains")
	ErrProtect       = errors.New("protection needs a username and a password of at least 8 characters")
	ErrEnvKey        = errors.New("env keys are letters, digits and underscores and do not start with a digit")
	ErrEnvValue      = errors.New("env values are at most 32 KB")
	ErrEnvLimit      = errors.New("an app has at most 200 env vars")
)

const deployHookMarker = "dph_"

var (
	imagePattern        = regexp.MustCompile(`^[a-z0-9][A-Za-z0-9._/:@-]{0,254}$`)
	registryHostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,252}[a-z0-9])?(:[0-9]{1,5})?$`)
	appNamePattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	domainPattern       = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	envKeyPattern       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// AppService manages apps, their deploys and their env. Deploys run on the
// agent; their log lines live in memory until the deploy ends.
type AppService struct {
	apps       repository.AppRepository
	servers    *ServerService
	box        *secret.Box
	github     *GitHub
	cloudflare *Cloudflare
	now        func() time.Time

	mu          sync.Mutex
	live        map[string]*liveDeploy
	subscribers map[string]map[chan struct{}]struct{}
}

// liveDeploy is a running deploy: its lines so far and who follows them.
type liveDeploy struct {
	workspaceID string
	serverID    string
	lines       []string
	followers   map[chan struct{}]struct{}
}

func NewAppService(apps repository.AppRepository, servers *ServerService, box *secret.Box) *AppService {
	service := &AppService{
		apps:        apps,
		servers:     servers,
		box:         box,
		cloudflare:  NewCloudflare(""),
		now:         time.Now,
		live:        map[string]*liveDeploy{},
		subscribers: map[string]map[chan struct{}]struct{}{},
	}
	servers.OnAgentMessage(service.receive)
	servers.OnAgentConnect(func(serverID, workspaceID string) {
		_, err := apps.BackupStorage(workspaceID)
		if err == nil {
			service.configureAgent(serverID, workspaceID)
		}
		service.configureServerOnConnect(serverID, workspaceID)
	})
	return service
}

// Create adds an app on one of the user's servers.
func (s *AppService) Create(actor Actor, name, serverIDOrName string) (*model.App, error) {
	name = strings.TrimSpace(name)
	if !appNamePattern.MatchString(name) {
		return nil, ErrAppName
	}
	server, err := s.servers.servers.ByIDOrNameForWorkspace(strings.TrimSpace(serverIDOrName), actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	app := &model.App{ID: uuid.New().String(), WorkspaceID: actor.WorkspaceID, ServerID: server.ID, Name: name, CreatedAt: s.now().UTC()}
	err = s.apps.Create(app)
	if err != nil {
		return nil, err
	}
	s.servers.record(actor, "app.created", "app", name, map[string]string{"server": server.Name})
	s.notify(actor.WorkspaceID)
	return s.apps.ByID(app.ID)
}

// List returns the user's apps with their newest and live deploy.
func (s *AppService) List(workspaceID string) ([]model.App, error) {
	return s.apps.ListByWorkspaceID(workspaceID)
}

// Get returns one app by ID or name.
func (s *AppService) Get(workspaceID, idOrName string) (*model.App, error) {
	return s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
}

// Delete stops the app on its server, then forgets it. Its data stays on
// the server. An offline server keeps the app, because its containers would
// keep running unseen.
func (s *AppService) Delete(ctx context.Context, actor Actor, idOrName string) error {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err = s.servers.Request(ctx, app.ServerID, protocol.TypeAppRemove, protocol.AppRemove{App: app.Name}, nil)
	if err != nil {
		return err
	}
	err = s.apps.DeleteForWorkspace(app.ID, actor.WorkspaceID)
	if err != nil {
		return err
	}
	s.servers.record(actor, "app.deleted", "app", app.Name, nil)
	s.notify(actor.WorkspaceID)
	return nil
}

// Deploys returns the newest deploys of an app, without logs.
func (s *AppService) Deploys(workspaceID, idOrName string, limit int) (*model.App, []model.Deploy, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	deploys, err := s.apps.ListDeploys(app.ID, min(max(limit, 1), 100))
	if err != nil {
		return nil, nil, err
	}
	for index := range deploys {
		s.expire(&deploys[index])
	}
	return app, deploys, nil
}

// Deploy deploys an image built elsewhere, such as in CI, as the main
// service; the server keeps the compose file of the running release.
func (s *AppService) Deploy(actor Actor, idOrName, image string) (*model.Deploy, error) {
	if !imagePattern.MatchString(image) {
		return nil, ErrImage
	}
	app, err := s.ready(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, err
	}
	deploy := s.newDeploy(app)
	deploy.Image = image
	return s.start(actor, "app.deployed", app, deploy, protocol.DeployStart{Image: image})
}

// newDeploy is a running deploy of app, not stored yet.
func (s *AppService) newDeploy(app *model.App) *model.Deploy {
	return &model.Deploy{ID: uuid.New().String(), AppID: app.ID, Status: model.DeployRunning, CreatedAt: s.now().UTC()}
}

// DeployHook deploys an image for the app whose deploy hook token this is.
// CI calls it after pushing the image.
func (s *AppService) DeployHook(token, image string) (*model.App, *model.Deploy, error) {
	if !strings.HasPrefix(token, deployHookMarker) {
		return nil, nil, ErrDeployHook
	}
	app, err := s.apps.ByDeployHookHash(hashToken(token))
	if errors.Is(err, repository.ErrAppNotFound) {
		return nil, nil, ErrDeployHook
	}
	if err != nil {
		return nil, nil, err
	}
	deploy, err := s.Deploy(Actor{WorkspaceID: app.WorkspaceID, Via: "hook"}, app.ID, image)
	return app, deploy, err
}

// RotateDeployHook gives the app a new deploy hook URL, shown once. The old
// one stops working.
func (s *AppService) RotateDeployHook(actor Actor, idOrName string) (string, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return "", err
	}
	token := deployHookMarker + randomToken()
	err = s.apps.SetDeployHookHash(app.ID, hashToken(token))
	if err != nil {
		return "", err
	}
	s.servers.record(actor, "app.hook_rotated", "app", app.Name, nil)
	return s.servers.appURL + "/webhooks/deploy/" + token, nil
}

// RegistryLogin is a registry login without its password.
type RegistryLogin struct {
	Host      string
	Username  string
	CreatedAt time.Time
}

// Registries lists the user's registry logins.
func (s *AppService) Registries(workspaceID string) ([]RegistryLogin, error) {
	registries, err := s.apps.Registries(workspaceID)
	if err != nil {
		return nil, err
	}
	logins := make([]RegistryLogin, 0, len(registries))
	for _, registry := range registries {
		logins = append(logins, RegistryLogin{Host: registry.Host, Username: registry.Username, CreatedAt: registry.CreatedAt})
	}
	return logins, nil
}

// SetRegistry stores a login agents use to pull the user's images from host.
func (s *AppService) SetRegistry(actor Actor, host, username, password string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if !registryHostPattern.MatchString(host) {
		return ErrRegistryHost
	}
	if strings.TrimSpace(username) == "" || password == "" {
		return ErrRegistryLogin
	}
	err := s.apps.SetRegistry(model.Registry{
		WorkspaceID: actor.WorkspaceID,
		Host:        host,
		Username:    strings.TrimSpace(username),
		Password:    s.box.Seal(password, actor.WorkspaceID+"/"+host),
		CreatedAt:   s.now().UTC(),
	})
	if err != nil {
		return err
	}
	s.servers.record(actor, "registry.set", "registry", host, map[string]string{"username": strings.TrimSpace(username)})
	return nil
}

// RemoveRegistry deletes a registry login.
func (s *AppService) RemoveRegistry(actor Actor, host string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	err := s.apps.DeleteRegistry(actor.WorkspaceID, host)
	if err != nil {
		return err
	}
	s.servers.record(actor, "registry.removed", "registry", host, nil)
	return nil
}

// registry returns the user's login for the registry of image, if any.
func (s *AppService) registry(workspaceID, image string) (*protocol.Registry, error) {
	host := registryHost(image)
	registries, err := s.apps.Registries(workspaceID)
	if err != nil {
		return nil, err
	}
	for _, registry := range registries {
		if registry.Host != host {
			continue
		}
		password, err := s.box.Open(registry.Password, workspaceID+"/"+host)
		if err != nil {
			return nil, fmt.Errorf("registry %s: %w", host, err)
		}
		return &protocol.Registry{Host: host, Username: registry.Username, Password: password}, nil
	}
	return nil, nil
}

// registryHost is the registry of an image reference: its first part when
// that looks like a host, else Docker Hub.
func registryHost(image string) string {
	first, _, found := strings.Cut(image, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}
	return "docker.io"
}

// Redeploy runs the newest deploy that had a source or an image again, with
// the env as it is now. Rollbacks are skipped.
func (s *AppService) Redeploy(actor Actor, idOrName string) (*model.Deploy, error) {
	app, err := s.ready(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, err
	}
	deploys, err := s.apps.ListDeploys(app.ID, 100)
	if err != nil {
		return nil, err
	}
	for _, previous := range deploys {
		if previous.SourceKey == "" && previous.Image == "" {
			continue
		}
		deploy := &model.Deploy{
			ID:           uuid.New().String(),
			AppID:        app.ID,
			Status:       model.DeployRunning,
			SourceKey:    previous.SourceKey,
			SourceSHA256: previous.SourceSHA256,
			Image:        previous.Image,
			CreatedAt:    s.now().UTC(),
		}
		// The server builds the source it kept of that release again.
		start := protocol.DeployStart{Image: deploy.Image}
		if deploy.SourceKey != "" {
			start.SourceNumber = previous.Number
		}
		return s.start(actor, "app.redeployed", app, deploy, start)
	}
	return nil, ErrNoSource
}

// Apply restarts the running release with the env and routing as they are
// now, without a build.
func (s *AppService) Apply(actor Actor, idOrName string) (*model.Deploy, error) {
	return s.apply(actor, "app.applied", idOrName)
}

func (s *AppService) apply(actor Actor, action, idOrName string) (*model.Deploy, error) {
	app, err := s.ready(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, err
	}
	if app.LiveNumber == 0 {
		return nil, ErrNoSource
	}
	deploy := &model.Deploy{ID: uuid.New().String(), AppID: app.ID, Status: model.DeployRunning, CreatedAt: s.now().UTC()}
	return s.start(actor, action, app, deploy, protocol.DeployStart{})
}

// SetDomains replaces the app's domains and the canonical one ("" for none),
// then applies them to the running release. The deploy is nil when there is
// nothing to apply yet, or the server is offline or busy: the next deploy
// takes them.
func (s *AppService) SetDomains(actor Actor, idOrName string, domains []string, canonical string) (*model.Deploy, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	clean := []string{}
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
		if domain == "" || slices.Contains(clean, domain) {
			continue
		}
		if !domainPattern.MatchString(domain) || len(domain) > 253 {
			return nil, ErrDomain
		}
		clean = append(clean, domain)
	}
	canonical = strings.ToLower(strings.TrimSpace(canonical))
	if canonical != "" && !slices.Contains(clean, canonical) {
		return nil, ErrCanonical
	}
	body, _ := json.Marshal(clean)
	err = s.apps.SetRouting(app.ID, string(body), canonical)
	if err != nil {
		return nil, err
	}
	app.Domains = string(body)
	s.syncTunnelOf(app)
	s.servers.record(actor, "app.domains_set", "app", app.Name, map[string]string{"domains": strings.Join(clean, ", "), "canonical": canonical})
	return s.applyIfLive(actor, app)
}

// SetProtection puts a password in front of the app, or takes it away when
// password is empty, then applies it to the running release.
func (s *AppService) SetProtection(actor Actor, idOrName, username, password string) (*model.Deploy, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	protect := ""
	if password != "" {
		username = strings.TrimSpace(username)
		if username == "" || strings.ContainsAny(username, ": ") || len(password) < 8 || len(password) > 72 {
			return nil, ErrProtect
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		protect = username + ":" + string(hash)
	}
	err = s.apps.SetProtect(app.ID, protect)
	if err != nil {
		return nil, err
	}
	action := "app.protected"
	if protect == "" {
		action = "app.unprotected"
	}
	s.servers.record(actor, action, "app", app.Name, nil)
	return s.applyIfLive(actor, app)
}

// applyIfLive applies a change to the running release. The change itself is
// in the activity, so the restart is not.
func (s *AppService) applyIfLive(actor Actor, app *model.App) (*model.Deploy, error) {
	s.notify(actor.WorkspaceID)
	if app.LiveNumber == 0 || !s.servers.Online(app.ServerID) {
		return nil, nil
	}
	deploy, err := s.apply(actor, "", app.ID)
	if errors.Is(err, ErrDeployRunning) || errors.Is(err, ErrServerOffline) {
		return nil, nil
	}
	return deploy, err
}

// DomainCheck says whether a domain points at the app's server.
type DomainCheck struct {
	Domain    string
	Canonical bool
	// Address is the server's address the domain should point at.
	Address string
	Found   []string
	Points  bool
}

// CheckDomains looks up every domain of an app and compares it with the
// address its agent connects from.
func (s *AppService) CheckDomains(ctx context.Context, workspaceID, idOrName string) ([]DomainCheck, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	server, err := s.servers.servers.ByIDOrNameForWorkspace(app.ServerID, workspaceID)
	if err != nil {
		return nil, err
	}
	checks := []DomainCheck{}
	for _, domain := range app.DomainList() {
		check := DomainCheck{Domain: domain, Canonical: domain == app.Canonical, Address: server.Address}
		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		check.Found, _ = net.DefaultResolver.LookupHost(lookupCtx, domain)
		cancel()
		check.Points = server.Address != "" && slices.Contains(check.Found, server.Address)
		if server.Tunnel {
			// ops points the domain at the tunnel itself; Cloudflare proxies
			// it, so it resolves to Cloudflare's addresses.
			check.Address = server.TunnelID + ".cfargotunnel.com"
			check.Points = len(check.Found) > 0
		}
		checks = append(checks, check)
	}
	return checks, nil
}

// Rollback starts the release of an earlier successful deploy again, with
// the env it had. Number 0 means the one before the live deploy.
func (s *AppService) Rollback(actor Actor, idOrName string, number int) (*model.Deploy, error) {
	app, err := s.ready(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, err
	}
	if number == 0 {
		deploys, err := s.apps.ListDeploys(app.ID, 100)
		if err != nil {
			return nil, err
		}
		for _, deploy := range deploys {
			if deploy.Status == model.DeploySucceeded && deploy.Number < app.LiveNumber {
				number = deploy.Number
				break
			}
		}
	}
	target, err := s.apps.Deploy(app.ID, number)
	if errors.Is(err, repository.ErrDeployNotFound) || (err == nil && target.Status != model.DeploySucceeded) {
		return nil, ErrNoRollback
	}
	if err != nil {
		return nil, err
	}
	deploy := &model.Deploy{
		ID:         uuid.New().String(),
		AppID:      app.ID,
		Status:     model.DeployRunning,
		FromNumber: target.Number,
		CreatedAt:  s.now().UTC(),
	}
	return s.start(actor, "app.rolled_back", app, deploy, protocol.DeployStart{FromNumber: target.Number})
}

// ready returns an app whose server is online and that has no running deploy.
func (s *AppService) ready(workspaceID, idOrName string) (*model.App, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	if !s.servers.Online(app.ServerID) {
		return nil, ErrServerOffline
	}
	if app.LatestStatus == model.DeployRunning {
		latest, err := s.apps.Deploy(app.ID, app.LatestNumber)
		if err != nil {
			return nil, err
		}
		if !s.expire(latest) {
			return nil, ErrDeployRunning
		}
	}
	return app, nil
}

// start stores the deploy, sends it to the agent and records action in the
// activity; an empty action records nothing.
func (s *AppService) start(actor Actor, action string, app *model.App, deploy *model.Deploy, start protocol.DeployStart) (*model.Deploy, error) {
	env, err := s.Env(app.WorkspaceID, app.ID)
	if err != nil {
		return nil, err
	}
	if start.Image != "" {
		start.Registry, err = s.registry(app.WorkspaceID, start.Image)
		if err != nil {
			return nil, err
		}
	}
	err = s.apps.CreateDeploy(deploy)
	if err != nil {
		return nil, err
	}
	start.DeployID, start.App, start.Number = deploy.ID, app.Name, deploy.Number
	start.Routing = &protocol.Routing{Domains: app.DomainList(), Canonical: app.Canonical, Protect: app.Protect}
	start.Snapshot = s.snapshotBeforeWrite(app)
	if start.FromNumber == 0 {
		start.Env = env
	}
	s.mu.Lock()
	s.live[deploy.ID] = &liveDeploy{workspaceID: app.WorkspaceID, serverID: app.ServerID, followers: map[chan struct{}]struct{}{}}
	s.mu.Unlock()
	s.notify(app.WorkspaceID)

	err = s.servers.Send(app.ServerID, protocol.TypeDeployStart, start)
	if err != nil {
		s.finish(deploy.ID, protocol.DeployDone{DeployID: deploy.ID, Error: "the server went offline before the deploy started"})
		return nil, ErrServerOffline
	}
	slog.Info("deploy started", "app_id", app.ID, "number", deploy.Number, "from_number", deploy.FromNumber)
	if action != "" {
		details := map[string]string{"number": strconv.Itoa(deploy.Number)}
		for key, value := range map[string]string{"image": deploy.Image, "ref": deploy.Ref, "commit": clipText(deploy.CommitSHA, 7), "message": deploy.CommitMessage} {
			if value != "" {
				details[key] = value
			}
		}
		if deploy.FromNumber > 0 {
			details["from"] = strconv.Itoa(deploy.FromNumber)
		}
		s.servers.record(actor, action, "app", app.Name, details)
	}
	return deploy, nil
}

// expire fails a running deploy the agent stopped reporting on, and reports
// whether the deploy is over.
func (s *AppService) expire(deploy *model.Deploy) bool {
	if deploy.Status != model.DeployRunning {
		return true
	}
	if s.now().Sub(deploy.CreatedAt) < deployTimeout {
		return false
	}
	s.finish(deploy.ID, protocol.DeployDone{DeployID: deploy.ID, Error: "the agent stopped reporting on this deploy"})
	deploy.Status = model.DeployFailed
	return true
}

// receive takes the deploy messages of an agent. A server can only report on
// deploys of its own apps.
func (s *AppService) receive(serverID string, message protocol.Message) {
	switch message.Type {
	case protocol.TypeDeployLog:
		var line protocol.DeployLog
		if message.Decode(&line) != nil {
			return
		}
		s.mu.Lock()
		live := s.live[line.DeployID]
		if live != nil && live.serverID == serverID && len(live.lines) < maxDeployLogLines {
			live.lines = append(live.lines, line.Line)
			for follower := range live.followers {
				select {
				case follower <- struct{}{}:
				default:
				}
			}
		}
		s.mu.Unlock()
	case protocol.TypeBackupStatus:
		var backups protocol.Backups
		if message.Decode(&backups) == nil {
			s.receiveBackupStatus(serverID, backups)
		}
	case protocol.TypeDeployDone:
		var done protocol.DeployDone
		if message.Decode(&done) != nil {
			return
		}
		deploy, err := s.apps.DeployByID(done.DeployID)
		if err != nil {
			return
		}
		app, err := s.apps.ByID(deploy.AppID)
		if err != nil || app.ServerID != serverID {
			slog.Warn("agent reported on a deploy of another server", "server_id", serverID, "deploy_id", done.DeployID)
			return
		}
		s.finish(done.DeployID, done)
	}
}

// finish stores how a deploy ended with its log and wakes its followers.
func (s *AppService) finish(deployID string, done protocol.DeployDone) {
	s.mu.Lock()
	live := s.live[deployID]
	delete(s.live, deployID)
	s.mu.Unlock()

	status := model.DeployFailed
	if done.OK {
		status = model.DeploySucceeded
	}
	detected := ""
	if done.Detected.Kind != "" {
		body, err := json.Marshal(done.Detected)
		if err == nil {
			detected = string(body)
		}
	}
	log := ""
	if live != nil {
		log = strings.Join(live.lines, "\n")
	}
	err := s.apps.FinishDeploy(deployID, status, clipText(done.Error, 2000), log, detected, s.now().UTC())
	if err != nil {
		slog.Error("store deploy result failed", "error", err, "deploy_id", deployID)
	}
	if live != nil {
		s.mu.Lock()
		for follower := range live.followers {
			close(follower)
		}
		s.mu.Unlock()
		s.notify(live.workspaceID)
	}
	s.reportFinished(deployID, status, done.Error)
	if status == model.DeploySucceeded && len(done.Detected.Domains) > 0 {
		deploy, err := s.apps.DeployByID(deployID)
		if err == nil {
			app, err := s.apps.ByID(deploy.AppID)
			if err == nil {
				s.syncTunnelOf(app)
			}
		}
	}
}

// reportFinished writes the commit status of a finished deploy that came
// from GitHub.
func (s *AppService) reportFinished(deployID, status, failure string) {
	deploy, err := s.apps.DeployByID(deployID)
	if err != nil || deploy.CommitSHA == "" {
		return
	}
	app, err := s.apps.ByID(deploy.AppID)
	if err != nil {
		return
	}
	state, description := "success", fmt.Sprintf("v%d is live", deploy.Number)
	if status != model.DeploySucceeded {
		state, description = "failure", fmt.Sprintf("v%d failed: %s", deploy.Number, failure)
	}
	go s.reportStatus(app, deploy, state, description)
}

// Follow returns the lines of a deploy so far. While it runs, changes gets a
// value after new lines and closes when it ends; for a finished deploy
// changes is nil.
func (s *AppService) Follow(workspaceID, idOrName string, number int) (deploy *model.Deploy, lines func() []string, changes <-chan struct{}, stop func(), err error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	deploy, err = s.apps.Deploy(app.ID, number)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	s.expire(deploy)
	s.mu.Lock()
	defer s.mu.Unlock()
	live := s.live[deploy.ID]
	if live == nil {
		// It may have ended a moment ago; read it again for the log.
		deploy, err = s.apps.Deploy(app.ID, number)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		all := splitLines(deploy.Log)
		return deploy, func() []string { return all }, nil, func() {}, nil
	}
	follower := make(chan struct{}, 1)
	live.followers[follower] = struct{}{}
	lines = func() []string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return slices.Clone(live.lines)
	}
	stop = func() {
		s.mu.Lock()
		delete(live.followers, follower)
		s.mu.Unlock()
	}
	return deploy, lines, follower, stop, nil
}

// DeployByNumber returns one deploy with its log.
func (s *AppService) DeployByNumber(workspaceID, idOrName string, number int) (*model.Deploy, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	deploy, err := s.apps.Deploy(app.ID, number)
	if err != nil {
		return nil, err
	}
	s.expire(deploy)
	return deploy, nil
}

// Logs asks the agent for the last lines of the app's containers.
func (s *AppService) Logs(ctx context.Context, workspaceID, idOrName string, tail int) ([]string, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var result protocol.LogsResult
	err = s.servers.Request(ctx, app.ServerID, protocol.TypeLogs, protocol.LogsRequest{App: app.Name, Tail: tail}, &result)
	return result.Lines, err
}

// Env returns the app's env vars in plain text. The next deploy gets them.
func (s *AppService) Env(workspaceID, idOrName string) (map[string]string, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	sealed, err := s.apps.Env(app.ID)
	if err != nil {
		return nil, err
	}
	env := make(map[string]string, len(sealed))
	for key, value := range sealed {
		env[key], err = s.box.Open(value, app.ID+"/"+key)
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", key, err)
		}
	}
	return env, nil
}

// SetEnv sets and removes env vars. They take effect with the next deploy.
func (s *AppService) SetEnv(actor Actor, idOrName string, set map[string]string, unset []string) error {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return err
	}
	current, err := s.apps.Env(app.ID)
	if err != nil {
		return err
	}
	sealed := make(map[string]string, len(set))
	for key, value := range set {
		if !envKeyPattern.MatchString(key) {
			return ErrEnvKey
		}
		if len(value) > maxEnvValueSize {
			return ErrEnvValue
		}
		sealed[key] = s.box.Seal(value, app.ID+"/"+key)
		current[key] = ""
	}
	for _, key := range unset {
		if !envKeyPattern.MatchString(key) {
			return ErrEnvKey
		}
		delete(current, key)
	}
	if len(current) > maxEnvVars {
		return ErrEnvLimit
	}
	err = s.apps.SetEnv(app.ID, sealed, unset, s.now().UTC())
	if err != nil {
		return err
	}
	// The activity names the keys, never the values.
	keys := slices.Sorted(maps.Keys(set))
	if len(keys) > 0 {
		s.servers.record(actor, "env.set", "app", app.Name, map[string]string{"keys": strings.Join(keys, ", ")})
	}
	if len(unset) > 0 {
		s.servers.record(actor, "env.removed", "app", app.Name, map[string]string{"keys": strings.Join(unset, ", ")})
	}
	s.notify(actor.WorkspaceID)
	return nil
}

// Subscribe returns a channel that receives a value whenever an app of the
// user or one of its deploys changes.
func (s *AppService) Subscribe(workspaceID string) (<-chan struct{}, func()) {
	changes := make(chan struct{}, 1)
	s.mu.Lock()
	if s.subscribers[workspaceID] == nil {
		s.subscribers[workspaceID] = map[chan struct{}]struct{}{}
	}
	s.subscribers[workspaceID][changes] = struct{}{}
	s.mu.Unlock()
	return changes, func() {
		s.mu.Lock()
		delete(s.subscribers[workspaceID], changes)
		if len(s.subscribers[workspaceID]) == 0 {
			delete(s.subscribers, workspaceID)
		}
		s.mu.Unlock()
	}
}

func (s *AppService) notify(workspaceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for changes := range s.subscribers[workspaceID] {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
}

// Detected decodes what the agent reported for a deploy.
func Detected(deploy model.Deploy) *protocol.Detected {
	if deploy.Detected == "" {
		return nil
	}
	var detected protocol.Detected
	if json.Unmarshal([]byte(deploy.Detected), &detected) != nil {
		return nil
	}
	return &detected
}

func splitLines(text string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

func clipText(value string, limit int) string {
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

// Actor is who changes something in a workspace: a member through the web,
// the CLI or an agent, or GitHub for a push. Via names the way in: web, api,
// mcp, hook or github.
type Actor struct {
	WorkspaceID string
	UserID      string
	Via         string
}
