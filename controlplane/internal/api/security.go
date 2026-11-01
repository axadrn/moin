package api

import (
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
)

// Integrations is the answer of GET /api/v1/integrations: which keys are
// stored, never the keys.
type Integrations struct {
	Cloudflare        bool   `json:"cloudflare"`
	CloudflareAccount string `json:"cloudflare_account"`
	Tailscale         bool   `json:"tailscale"`
}

// IntegrationsRequest is the body of PUT /api/v1/integrations. A missing
// key stays, an empty one is removed.
type IntegrationsRequest struct {
	CloudflareToken *string `json:"cloudflare_token"`
	TailscaleKey    *string `json:"tailscale_key"`
}

// SecurityRequest is the body of PUT /api/v1/servers/{id}/security.
type SecurityRequest struct {
	Tunnel       bool `json:"tunnel"`
	SSHTailscale bool `json:"ssh_tailscale"`
	Firewall     bool `json:"firewall"`
}

// GetIntegrations says which keys are stored.
func (h *AppsHandler) GetIntegrations(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	integrations, err := h.apps.Integrations(workspace.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, Integrations{Cloudflare: integrations.Cloudflare, CloudflareAccount: integrations.CloudflareAccount, Tailscale: integrations.Tailscale})
}

// SetIntegrations stores the Cloudflare API token and the Tailscale key.
func (h *AppsHandler) SetIntegrations(w http.ResponseWriter, r *http.Request) {
	var request IntegrationsRequest
	if !Decode(w, r, &request) {
		return
	}
	err := h.apps.SetIntegrations(r.Context(), actor(r), request.CloudflareToken, request.TailscaleKey)
	if err != nil {
		ServiceError(w, err)
		return
	}
	h.GetIntegrations(w, r)
}

// SetSecurity sets how a server is reached and locked down, and answers
// the server with what its agent set up.
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
