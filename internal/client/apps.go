package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/axadrn/moin/internal/dotenv"
	"github.com/axadrn/moin/protocol"
)

// App is a compose project on a server, as the API answers it.
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
	Protected    bool     `json:"protected"`
	// ServiceDomains are the domains of the app's other services.
	ServiceDomains map[string][]string `json:"service_domains"`
	// Instant is the instant address, "" when the app has none.
	Instant         string `json:"instant"`
	InstantProvider string `json:"instant_provider"`
	InstantOpen     bool   `json:"instant_open"`
	Stopped         bool   `json:"stopped"`
	// ChangesWaiting counts the changes saved since the live deploy
	// started; a restart applies them. Only App fills it.
	ChangesWaiting int `json:"changes_waiting"`
	// Repo and Branch are the GitHub repo the app follows, if any.
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	AutoDeploy bool   `json:"auto_deploy"`
	Pinned     bool   `json:"pinned"`
	// Hook says the app has a deploy hook URL.
	Hook bool `json:"hook"`
	// DeployFrom is ci or github, where new versions come from; empty
	// while neither is set up.
	DeployFrom string `json:"deploy_from"`
	// HookImage is the image the hook deploys new tags of, the one running
	// now; empty while the app runs from its repo.
	HookImage string `json:"hook_image"`
	// Bucket is the bucket the app backs up to; empty follows
	// the workspace's default.
	Bucket string `json:"bucket"`
	// Catalog is the catalog entry the app was installed from; empty for
	// an app of your own.
	Catalog   string    `json:"catalog"`
	CreatedAt time.Time `json:"created_at"`
	// InstantPassword is the password of the instant address, only in the
	// answer that created the app: the one time moin shows it.
	InstantPassword string `json:"instant_password"`
}

// Deploy is one version of an app, v12.
type Deploy struct {
	Number     int    `json:"number"`
	Status     string `json:"status"`
	FromNumber int    `json:"from_number"`
	Image      string `json:"image"`
	// Ref and Commit name the GitHub commit the deploy came from.
	Ref           string             `json:"ref"`
	Commit        string             `json:"commit"`
	CommitMessage string             `json:"commit_message"`
	Error         string             `json:"error"`
	Detected      *protocol.Detected `json:"detected"`
	CreatedAt     time.Time          `json:"created_at"`
	FinishedAt    *time.Time         `json:"finished_at"`
	Log           []string           `json:"log"`
}

// AppDetail is an app with its newest deploys.
type AppDetail struct {
	App     App      `json:"app"`
	Deploys []Deploy `json:"deploys"`
}

func appPath(app string, rest ...string) string {
	path := "/api/v1/apps/" + url.PathEscape(app)
	for _, part := range rest {
		path += "/" + part
	}
	return path
}

// Apps lists the apps of the account.
func (c *Client) Apps(ctx context.Context) ([]App, error) {
	var response struct {
		Items []App `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/apps", nil, &response)
	return response.Items, err
}

// App returns an app with its newest deploys.
func (c *Client) App(ctx context.Context, app string) (AppDetail, error) {
	var response AppDetail
	err := c.do(ctx, http.MethodGet, appPath(app), nil, &response)
	return response, err
}

// RenameApp gives an app a new name. app is its name, ID or link.
func (c *Client) RenameApp(ctx context.Context, app, name string) (App, error) {
	var response App
	err := c.do(ctx, http.MethodPatch, appPath(app), map[string]string{"name": name}, &response)
	return response, err
}

// AddApp creates an app on a server.
func (c *Client) AddApp(ctx context.Context, name, server string) (App, error) {
	var response App
	err := c.do(ctx, http.MethodPost, "/api/v1/apps", map[string]string{"name": name, "server": server}, &response)
	return response, err
}

// RemoveApp stops an app and removes it. Its data stays on the server.
// confirm is the app's name, typed out; the hub checks it.
func (c *Client) RemoveApp(ctx context.Context, app, confirm string) error {
	return c.do(ctx, http.MethodDelete, appPath(app)+"?confirm="+url.QueryEscape(confirm), nil, nil)
}

// Rollback starts an earlier successful release again. Number 0 is the one
// before the live deploy.
func (c *Client) Rollback(ctx context.Context, app string, number int) (Deploy, error) {
	var response Deploy
	err := c.do(ctx, http.MethodPost, appPath(app, "rollback"), map[string]int{"number": number}, &response)
	return response, err
}

// Redeploy builds the newest source again with the env as it is now.
func (c *Client) Redeploy(ctx context.Context, app string) (Deploy, error) {
	var response Deploy
	err := c.do(ctx, http.MethodPost, appPath(app, "redeploy"), nil, &response)
	return response, err
}

// GetDeploy returns one deploy with its whole log.
func (c *Client) GetDeploy(ctx context.Context, app string, number int) (Deploy, error) {
	var response Deploy
	err := c.do(ctx, http.MethodGet, appPath(app, "deploys", strconv.Itoa(number)), nil, &response)
	return response, err
}

// FollowDeploy calls line for every log line of a deploy and returns the
// deploy once it ended.
func (c *Client) FollowDeploy(ctx context.Context, app string, number int, line func(string)) (Deploy, error) {
	var done Deploy
	err := c.stream(ctx, appPath(app, "deploys", strconv.Itoa(number), "events"), func(event string, data []byte) error {
		switch event {
		case "line":
			var payload struct {
				Line string `json:"line"`
			}
			err := json.Unmarshal(data, &payload)
			if err == nil {
				line(payload.Line)
			}
			return err
		case "done":
			err := json.Unmarshal(data, &done)
			if err != nil {
				return err
			}
			return errStreamDone
		}
		return nil
	})
	if err == nil && done.Number == 0 {
		return done, fmt.Errorf("the deploy stream ended early")
	}
	return done, err
}

// Logs returns the last lines of an app's containers.
func (c *Client) Logs(ctx context.Context, app string, tail int) ([]string, error) {
	var response struct {
		Lines []string `json:"lines"`
	}
	err := c.do(ctx, http.MethodGet, appPath(app, "logs")+"?tail="+strconv.Itoa(tail), nil, &response)
	return response.Lines, err
}

// EnvVar is an env var of an app: its key and when it changed. Values are
// write-only: the hub never answers one.
type EnvVar struct {
	Key       string    `json:"key"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Env lists the env vars of an app, by key.
func (c *Client) Env(ctx context.Context, app string) ([]EnvVar, error) {
	var response struct {
		Items []EnvVar `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, appPath(app, "env"), nil, &response)
	return response.Items, err
}

// SetEnv sets and removes env vars and answers them all, by key. The next
// deploy gets them.
func (c *Client) SetEnv(ctx context.Context, app string, set map[string]string, unset []string) ([]EnvVar, error) {
	var response struct {
		Items []EnvVar `json:"items"`
	}
	err := c.do(ctx, http.MethodPatch, appPath(app, "env"), map[string]any{"set": set, "unset": unset}, &response)
	return response.Items, err
}

// ReplaceEnv makes env every env var of the app, like one .env file: keys
// not in env are removed. It answers what changed, by key. The next deploy
// gets them.
func (c *Client) ReplaceEnv(ctx context.Context, app string, env map[string]string) (dotenv.Change, error) {
	var response dotenv.Change
	err := c.do(ctx, http.MethodPut, appPath(app, "env"), map[string]any{"env": env}, &response)
	return response, err
}

// WatchApps calls fn with the full app list on connect and on every change.
func (c *Client) WatchApps(ctx context.Context, fn func([]App)) error {
	return c.stream(ctx, "/api/v1/apps/events", func(_ string, data []byte) error {
		var event struct {
			Items []App `json:"items"`
		}
		err := json.Unmarshal(data, &event)
		if err == nil {
			fn(event.Items)
		}
		return err
	})
}

// Status is "live v3", "deploying v4", "failed v4 · v3 live" or "new".
func (a App) Status() string {
	switch {
	case a.LatestNumber == 0:
		return "new"
	case a.LatestStatus == "running":
		return fmt.Sprintf("deploying v%d", a.LatestNumber)
	case a.Stopped:
		return fmt.Sprintf("stopped v%d", a.LiveNumber)
	case a.LatestStatus == "failed" && a.LiveNumber > 0:
		return fmt.Sprintf("failed v%d · v%d live", a.LatestNumber, a.LiveNumber)
	case a.LatestStatus == "failed":
		return fmt.Sprintf("failed v%d", a.LatestNumber)
	}
	return fmt.Sprintf("live v%d", a.LiveNumber)
}

// DeployImage deploys an image built elsewhere as the app's main service.
// An app that follows a GitHub repo takes the compose file of ref there;
// an empty ref is the linked branch.
func (c *Client) DeployImage(ctx context.Context, app, image, ref string) (Deploy, error) {
	var response Deploy
	err := c.do(ctx, http.MethodPost, appPath(app, "deploys"), map[string]string{"image": image, "ref": ref}, &response)
	return response, err
}

// DeployRef deploys a branch, tag or commit of the app's GitHub repo;
// an empty ref deploys the linked branch.
func (c *Client) DeployRef(ctx context.Context, app, ref string) (Deploy, error) {
	var response Deploy
	err := c.do(ctx, http.MethodPost, appPath(app, "deploys"), map[string]string{"ref": ref}, &response)
	return response, err
}

// RotateDeployHook gives the app a new deploy hook URL; the old one stops working.
func (c *Client) RotateDeployHook(ctx context.Context, app string) (string, error) {
	var response struct {
		URL string `json:"url"`
	}
	err := c.do(ctx, http.MethodPost, appPath(app, "hook"), nil, &response)
	return response.URL, err
}

// RemoveDeployHook turns the deploy hook off; its URL stops working.
func (c *Client) RemoveDeployHook(ctx context.Context, app string) error {
	return c.do(ctx, http.MethodDelete, appPath(app, "hook"), nil, nil)
}

// Registry is a registry login without its password, which only the
// servers keep: Servers say which, MissingOn which do not.
type Registry struct {
	Host      string     `json:"host"`
	Username  string     `json:"username"`
	CreatedAt time.Time  `json:"created_at"`
	Servers   []SecretOn `json:"servers"`
	MissingOn []string   `json:"missing_on"`
}

// SecretOn is a server that keeps a secret, and since when.
type SecretOn struct {
	Server    string    `json:"server"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Registries lists the workspace's registry logins.
func (c *Client) Registries(ctx context.Context) ([]Registry, error) {
	var response struct {
		Items []Registry `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/registries", nil, &response)
	return response.Items, err
}

// SetRegistry hands the login for a registry host to a server, which
// keeps it; an empty server is the workspace's only one.
func (c *Client) SetRegistry(ctx context.Context, host, username, password, server string) error {
	return c.do(ctx, http.MethodPut, "/api/v1/registries/"+url.PathEscape(host), map[string]string{"username": username, "password": password, "server": server}, nil)
}

// RemoveRegistry deletes the login for a registry host.
func (c *Client) RemoveRegistry(ctx context.Context, host string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/registries/"+url.PathEscape(host), nil, nil)
}

// DomainCheck says whether a domain points at the app's server.
type DomainCheck struct {
	Domain    string   `json:"domain"`
	Canonical bool     `json:"canonical"`
	Address   string   `json:"address"`
	IPv4      string   `json:"ipv4"`
	IPv6      string   `json:"ipv6"`
	Advice    string   `json:"advice"`
	Found     []string `json:"found"`
	Points    bool     `json:"points"`
	// Service is the service of compose.yml the domain reaches, empty for
	// the main one.
	Service string `json:"service"`
}

// Change is the app after a change of the domains, and the deploy that applies it,
// nil when the next deploy will.
type Change struct {
	App    App     `json:"app"`
	Deploy *Deploy `json:"deploy"`
	// Unchanged is set when the app had it so already; nothing restarts.
	Unchanged bool `json:"unchanged"`
}

// CheckDomains looks up every domain of an app.
func (c *Client) CheckDomains(ctx context.Context, app string) ([]DomainCheck, error) {
	var response struct {
		Items []DomainCheck `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, appPath(app, "domains"), nil, &response)
	return response.Items, err
}

// SetDomains replaces the domains and the canonical one ("" for none).
func (c *Client) SetDomains(ctx context.Context, app string, domains []string, canonical string) (Change, error) {
	var response Change
	err := c.do(ctx, http.MethodPut, appPath(app, "domains"), map[string]any{"domains": domains, "canonical": canonical}, &response)
	return response, err
}

// SetServiceDomains replaces the domains of another service of the app.
func (c *Client) SetServiceDomains(ctx context.Context, app, service string, domains []string) (Change, error) {
	var response Change
	err := c.do(ctx, http.MethodPut, appPath(app, "domains"), map[string]any{"domains": domains, "service": service}, &response)
	return response, err
}

// Instant is the app's instant address, or why it has none.
type Instant struct {
	URL         string `json:"url"`
	Provider    string `json:"provider"`
	Reason      string `json:"reason"`
	Open        bool   `json:"open"`
	User        string `json:"user"`
	OwnPassword bool   `json:"own_password"`
}

// Instant reports the app's instant address. moin never shows its
// password again: NewInstantPassword makes a new one.
func (c *Client) Instant(ctx context.Context, app string) (Instant, error) {
	var response Instant
	err := c.do(ctx, http.MethodGet, appPath(app, "instant"), nil, &response)
	return response, err
}

// InstantPassword is a new password of the instant address, shown once.
type InstantPassword struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

// NewInstantPassword makes a new password for the instant address: the
// one time moin shows it. The app takes it on its next restart.
func (c *Client) NewInstantPassword(ctx context.Context, app string) (InstantPassword, error) {
	var response InstantPassword
	err := c.do(ctx, http.MethodPost, appPath(app, "instant", "password"), nil, &response)
	return response, err
}

// SetInstant picks the provider of the instant address or off ("" keeps
// it), and whether it is open (nil keeps it).
func (c *Client) SetInstant(ctx context.Context, app, provider string, open *bool) (Change, error) {
	var response Change
	body := map[string]any{"provider": provider}
	if open != nil {
		body["open"] = *open
	}
	err := c.do(ctx, http.MethodPut, appPath(app, "instant"), body, &response)
	return response, err
}

// SetProtection puts a password in front of the app.
func (c *Client) SetProtection(ctx context.Context, app, username, password string) (Change, error) {
	var response Change
	err := c.do(ctx, http.MethodPut, appPath(app, "protection"), map[string]string{"username": username, "password": password}, &response)
	return response, err
}

// RemoveProtection takes the password away.
func (c *Client) RemoveProtection(ctx context.Context, app string) (Change, error) {
	var response Change
	err := c.do(ctx, http.MethodDelete, appPath(app, "protection"), nil, &response)
	return response, err
}

// Restart starts the live deploy again with the env and domains as they
// are now.
func (c *Client) Restart(ctx context.Context, app string) (Deploy, error) {
	var response Deploy
	err := c.do(ctx, http.MethodPost, appPath(app, "restart"), nil, &response)
	return response, err
}

// Start runs a stopped app again.
func (c *Client) Start(ctx context.Context, app string) (Deploy, error) {
	var response Deploy
	err := c.do(ctx, http.MethodPost, appPath(app, "start"), nil, &response)
	return response, err
}

// Stop takes the app offline until it starts again. Its data stays.
func (c *Client) Stop(ctx context.Context, app string) error {
	return c.do(ctx, http.MethodPost, appPath(app, "stop"), nil, nil)
}
