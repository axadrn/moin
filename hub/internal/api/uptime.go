package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
)

// Uptime is the answer of GET /api/v1/apps/{app}/uptime: the state of the
// app as a whole, its server, and each domain.
type Uptime struct {
	App string `json:"app"`
	// State is up, down, pending, paused, stopped or no_domains.
	State        string         `json:"state"`
	Server       string         `json:"server"`
	ServerOnline bool           `json:"server_online"`
	Domains      []UptimeDomain `json:"domains"`
}

// UptimeDomain is the uptime of one domain.
type UptimeDomain struct {
	Domain string `json:"domain"`
	// Path is where the visit goes; custom_path says it was set, not the
	// health path or /.
	Path       string `json:"path"`
	CustomPath bool   `json:"custom_path"`
	Paused     bool   `json:"paused"`
	// State is up, down, pending or paused, since when it began.
	State         string     `json:"state"`
	Since         *time.Time `json:"since"`
	VisitedAt     *time.Time `json:"visited_at"`
	StatusCode    int        `json:"status_code"`
	ResponseMS    int        `json:"response_ms"`
	Error         string     `json:"error"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
	// Uptime in percent; null without a visit in that time.
	Uptime24h *float64 `json:"uptime_24h"`
	Uptime7d  *float64 `json:"uptime_7d"`
	Uptime30d *float64 `json:"uptime_30d"`
	AverageMS int      `json:"average_ms"`
	Outages   []Outage `json:"outages"`
}

// Outage is a time a domain was down; ended_at is null while it lasts.
type Outage struct {
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Reason    string     `json:"reason"`
}

// UptimeRequest is the body of PUT /api/v1/apps/{app}/uptime/{domain}.
// A missing field keeps its setting; an empty path goes back to the health
// path or /.
type UptimeRequest struct {
	Path   *string `json:"path"`
	Paused *bool   `json:"paused"`
}

// NewUptime is the JSON shape of an app's uptime.
func NewUptime(app string, uptime service.AppUptime) Uptime {
	answer := Uptime{App: app, State: uptime.State, Server: uptime.Server, ServerOnline: uptime.ServerOnline, Domains: []UptimeDomain{}}
	for _, domain := range uptime.Domains {
		answer.Domains = append(answer.Domains, NewUptimeDomain(domain))
	}
	return answer
}

// NewUptimeDomain is the JSON shape of a domain's uptime.
func NewUptimeDomain(domain service.UptimeDomain) UptimeDomain {
	answer := UptimeDomain{
		Domain: domain.Domain, Path: domain.Path, CustomPath: domain.CustomPath, Paused: domain.Paused,
		State: domain.State, Since: domain.Since, VisitedAt: domain.VisitedAt, StatusCode: domain.StatusCode,
		ResponseMS: domain.ResponseMS, Error: domain.Error, CertExpiresAt: domain.CertExpiresAt,
		Uptime24h: domain.Uptime24h, Uptime7d: domain.Uptime7d, Uptime30d: domain.Uptime30d, AverageMS: domain.AverageMS,
		Outages: []Outage{},
	}
	for _, outage := range domain.Outages {
		answer.Outages = append(answer.Outages, Outage{StartedAt: outage.StartedAt, EndedAt: outage.EndedAt, Reason: outage.Reason})
	}
	return answer
}

// Uptime returns the uptime of every domain of an app.
func (h *AppsHandler) Uptime(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, uptime, err := h.apps.Uptime(workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewUptime(app.Name, uptime))
}

// SetUptime changes the path of a domain's visit or pauses it.
func (h *AppsHandler) SetUptime(w http.ResponseWriter, r *http.Request) {
	var request UptimeRequest
	if !Decode(w, r, &request) {
		return
	}
	_, domain, err := h.apps.SetUptime(actor(r), r.PathValue("app"), r.PathValue("domain"), request.Path, request.Paused)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewUptimeDomain(domain))
}
