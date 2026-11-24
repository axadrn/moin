package api

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
)

// Integrations is the answer of GET /api/v1/integrations: which keys there
// are, never the keys. The hub keeps the Cloudflare API token; the
// Tailscale key only the servers keep.
type Integrations struct {
	Cloudflare        bool          `json:"cloudflare"`
	CloudflareAccount string        `json:"cloudflare_account"`
	Tailscale         SecretServers `json:"tailscale"`
}

// IntegrationsRequest is the body of PUT /api/v1/integrations. A missing
// key stays, an empty one is removed. Server is the server that keeps the
// Tailscale key, empty for the workspace's only one.
type IntegrationsRequest struct {
	CloudflareToken *string `json:"cloudflare_token"`
	TailscaleKey    *string `json:"tailscale_key"`
	Server          string  `json:"server"`
}

// SecurityRequest is the body of PUT /api/v1/servers/{id}/security.
type SecurityRequest struct {
	Tunnel   bool   `json:"tunnel"`
	SSH      string `json:"ssh"`
	Firewall bool   `json:"firewall"`
}

// GetIntegrations says which keys are stored.
func (h *AppsHandler) GetIntegrations(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	integrations, err := h.apps.Integrations(workspace.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewIntegrations(integrations))
}

// NewIntegrations is the JSON shape of the workspace's keys.
func NewIntegrations(integrations service.Integrations) Integrations {
	return Integrations{Cloudflare: integrations.Cloudflare, CloudflareAccount: integrations.CloudflareAccount, Tailscale: NewSecretServers(integrations.Tailscale)}
}

// SetIntegrations stores the Cloudflare API token and hands the Tailscale
// key to a server.
func (h *AppsHandler) SetIntegrations(w http.ResponseWriter, r *http.Request) {
	var request IntegrationsRequest
	if !Decode(w, r, &request) {
		return
	}
	err := h.apps.SetIntegrations(r.Context(), actor(r), service.IntegrationsInput{CloudflareToken: request.CloudflareToken, TailscaleKey: request.TailscaleKey, TailscaleServer: request.Server})
	if err != nil {
		ServiceError(w, err)
		return
	}
	h.GetIntegrations(w, r)
}

// SetSecurity sets how a server is reached and locked down, and answers
// the server with what its daemon set up.
func (h *AppsHandler) SetSecurity(w http.ResponseWriter, r *http.Request) {
	var request SecurityRequest
	if !Decode(w, r, &request) {
		return
	}
	server, err := h.apps.SetServerSecurity(r.Context(), actor(r), r.PathValue("id"), service.SecurityInput(request))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewServer(*server))
}
