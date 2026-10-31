package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/axadrn/ops/protocol"
)

// App is a compose project on a server, as the API answers it.
type App struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ServerID     string    `json:"server_id"`
	ServerName   string    `json:"server_name"`
	LatestNumber int       `json:"latest_number"`
	LatestStatus string    `json:"latest_status"`
	LiveNumber   int       `json:"live_number"`
	CreatedAt    time.Time `json:"created_at"`
}

// Deploy is one release of an app.
type Deploy struct {
	Number     int                `json:"number"`
	Status     string             `json:"status"`
	FromNumber int                `json:"from_number"`
	Error      string             `json:"error"`
	Detected   *protocol.Detected `json:"detected"`
	CreatedAt  time.Time          `json:"created_at"`
	FinishedAt *time.Time         `json:"finished_at"`
	Log        []string           `json:"log"`
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

// AddApp creates an app on a server.
func (c *Client) AddApp(ctx context.Context, name, server string) (App, error) {
	var response App
	err := c.do(ctx, http.MethodPost, "/api/v1/apps", map[string]string{"name": name, "server": server}, &response)
	return response, err
}

// RemoveApp stops an app and removes it. Its data stays on the server.
func (c *Client) RemoveApp(ctx context.Context, app string) error {
	return c.do(ctx, http.MethodDelete, appPath(app), nil, nil)
}

// Deploy uploads a gzipped source tarball and starts a deploy.
func (c *Client) Deploy(ctx context.Context, app string, source io.Reader) (Deploy, error) {
	return c.deploySource(ctx, appPath(app, "deploys"), source)
}

func (c *Client) deploySource(ctx context.Context, path string, source io.Reader) (Deploy, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+path, source)
	if err != nil {
		return Deploy{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/gzip")
	response, err := c.http.Do(request)
	if err != nil {
		return Deploy{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return Deploy{}, responseError(response)
	}
	var deploy Deploy
	err = json.NewDecoder(response.Body).Decode(&deploy)
	return deploy, err
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

// Env returns the env vars of an app.
func (c *Client) Env(ctx context.Context, app string) (map[string]string, error) {
	var response struct {
		Items map[string]string `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, appPath(app, "env"), nil, &response)
	return response.Items, err
}

// SetEnv sets and removes env vars. The next deploy gets them.
func (c *Client) SetEnv(ctx context.Context, app string, set map[string]string, unset []string) (map[string]string, error) {
	var response struct {
		Items map[string]string `json:"items"`
	}
	err := c.do(ctx, http.MethodPatch, appPath(app, "env"), map[string]any{"set": set, "unset": unset}, &response)
	return response.Items, err
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
	case a.LatestStatus == "failed" && a.LiveNumber > 0:
		return fmt.Sprintf("failed v%d · v%d live", a.LatestNumber, a.LiveNumber)
	case a.LatestStatus == "failed":
		return fmt.Sprintf("failed v%d", a.LatestNumber)
	}
	return fmt.Sprintf("live v%d", a.LiveNumber)
}

// DeployImage deploys an image built elsewhere as the app's main service.
func (c *Client) DeployImage(ctx context.Context, app, image string) (Deploy, error) {
	var response Deploy
	err := c.do(ctx, http.MethodPost, appPath(app, "deploys"), map[string]string{"image": image}, &response)
	return response, err
}

// DeploySourceWithImage uploads a source tarball whose main service runs image.
func (c *Client) DeploySourceWithImage(ctx context.Context, app, image string, source io.Reader) (Deploy, error) {
	return c.deploySource(ctx, appPath(app, "deploys")+"?image="+url.QueryEscape(image), source)
}

// RotateDeployHook gives the app a new deploy hook URL; the old one stops working.
func (c *Client) RotateDeployHook(ctx context.Context, app string) (string, error) {
	var response struct {
		URL string `json:"url"`
	}
	err := c.do(ctx, http.MethodPost, appPath(app, "hook"), nil, &response)
	return response.URL, err
}

// Registry is a registry login without its password.
type Registry struct {
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// Registries lists the account's registry logins.
func (c *Client) Registries(ctx context.Context) ([]Registry, error) {
	var response struct {
		Items []Registry `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/registries", nil, &response)
	return response.Items, err
}

// SetRegistry stores the login for a registry host.
func (c *Client) SetRegistry(ctx context.Context, host, username, password string) error {
	return c.do(ctx, http.MethodPut, "/api/v1/registries/"+url.PathEscape(host), map[string]string{"username": username, "password": password}, nil)
}

// RemoveRegistry deletes the login for a registry host.
func (c *Client) RemoveRegistry(ctx context.Context, host string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/registries/"+url.PathEscape(host), nil, nil)
}
