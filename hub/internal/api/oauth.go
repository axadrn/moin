package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/brand"
	"github.com/axadrn/moin/hub/internal/service"
)

// OAuthHandler tells MCP clients how to sign in and registers them. The two
// documents under /.well-known follow RFC 9728 and RFC 8414, registration
// RFC 7591: an MCP client finds all of it from the 401 of /mcp, with
// nothing to set up by hand.
type OAuthHandler struct {
	oauth *service.OAuthService
}

func NewOAuthHandler(oauth *service.OAuthService) *OAuthHandler {
	return &OAuthHandler{oauth: oauth}
}

// ProtectedResourceMetadata describes /mcp as a protected resource (RFC
// 9728): what it is and which authorization server hands out its tokens.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceName           string   `json:"resource_name"`
	ResourceDocumentation  string   `json:"resource_documentation"`
}

// AuthorizationServerMetadata describes the hub as an authorization server
// (RFC 8414). Only public clients with PKCE S256: no client has a secret.
type AuthorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RegistrationEndpoint                       string   `json:"registration_endpoint"`
	RevocationEndpoint                         string   `json:"revocation_endpoint"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported"`
	AuthorizationResponseISSParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
	ServiceDocumentation                       string   `json:"service_documentation"`
}

// ProtectedResourcePath is where /mcp's metadata is: the well-known path
// with the resource's path after it (RFC 9728, section 3.1).
const ProtectedResourcePath = "/.well-known/oauth-protected-resource" + service.MCPPath

// ProtectedResource answers the metadata of /mcp.
func (h *OAuthHandler) ProtectedResource(w http.ResponseWriter, r *http.Request) {
	issuer := h.oauth.Issuer()
	metadata(w, ProtectedResourceMetadata{
		Resource:               h.oauth.Resource(),
		AuthorizationServers:   []string{issuer},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           brand.Name,
		ResourceDocumentation:  issuer + "/docs/guides/mcp",
	})
}

// AuthorizationServer answers the metadata of the hub as an authorization
// server.
func (h *OAuthHandler) AuthorizationServer(w http.ResponseWriter, r *http.Request) {
	issuer := h.oauth.Issuer()
	metadata(w, AuthorizationServerMetadata{
		Issuer:                                     issuer,
		AuthorizationEndpoint:                      issuer + "/oauth/authorize",
		TokenEndpoint:                              issuer + "/api/v1/auth/token",
		RegistrationEndpoint:                       issuer + "/api/v1/auth/register",
		RevocationEndpoint:                         issuer + "/api/v1/auth/revoke",
		ResponseTypesSupported:                     []string{"code"},
		GrantTypesSupported:                        []string{CodeGrantType, RefreshGrantType},
		CodeChallengeMethodsSupported:              []string{"S256"},
		TokenEndpointAuthMethodsSupported:          []string{"none"},
		RevocationEndpointAuthMethodsSupported:     []string{"none"},
		AuthorizationResponseISSParameterSupported: true,
		ServiceDocumentation:                       issuer + "/docs/guides/mcp",
	})
}

// metadata answers a public metadata document, which any page may read.
func metadata(w http.ResponseWriter, document any) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	JSON(w, http.StatusOK, document)
}

// ClientRegistration is what an MCP client sends to register (RFC 7591,
// section 2). Only the name and the redirect URIs are kept; the rest is
// what every client gets here anyway.
type ClientRegistration struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

// RegisteredClient is the answer of a registration (RFC 7591, section
// 3.2.1): the client_id, and what the hub keeps of the request.
type RegisteredClient struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

const maxRegistration = 16 << 10

// Register stores an MCP client that registers itself. Every client is a
// public client with PKCE: one that asks for a secret gets none, as RFC
// 7591 lets the hub decide.
func (h *OAuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "Send the registration as application/json.")
		return
	}
	var request ClientRegistration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRegistration)).Decode(&request); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "The registration could not be read as JSON.")
		return
	}
	client, err := h.oauth.Register(service.NewClient{ClientName: request.ClientName, RedirectURIs: request.RedirectURIs})
	if errors.Is(err, service.ErrRedirectURIs) {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "Send a redirect URI with https, or with http on localhost, 127.0.0.1 or [::1].")
		return
	}
	if err != nil {
		slog.Error("client registration failed", "error", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "Something went wrong. Please try again.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	JSON(w, http.StatusCreated, RegisteredClient{
		ClientID:                client.ID,
		ClientIDIssuedAt:        client.CreatedAt.Unix(),
		ClientName:              client.ClientName,
		RedirectURIs:            client.Redirects(),
		GrantTypes:              []string{CodeGrantType, RefreshGrantType},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
	})
}
