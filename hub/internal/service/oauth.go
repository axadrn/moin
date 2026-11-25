package service

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/google/uuid"
)

const (
	// MCPPath is where the MCP server answers. The tokens of a connection
	// made by OAuth work only there.
	MCPPath = "/mcp"
	// maxRedirectURIs and maxRedirectURILength keep a registration small.
	maxRedirectURIs      = 10
	maxRedirectURILength = 2000
	// unusedClientLifetime is how long a client nobody approved stays.
	unusedClientLifetime = 24 * time.Hour
)

var (
	// ErrOAuthClient is a client_id this hub does not know.
	ErrOAuthClient = errors.New("unknown client")
	// ErrOAuthRedirect is a redirect URI that is missing or not one the
	// client registered. Nobody is sent there.
	ErrOAuthRedirect = errors.New("the redirect URI is not one the client registered")
	// ErrRedirectURIs is a registration without one redirect URI this hub
	// sends people to: https, or http on this computer.
	ErrRedirectURIs = errors.New("redirect URIs must be https, or http on localhost, 127.0.0.1 or [::1]")
	// ErrCodeInvalid is an authorization code that is unknown, expired,
	// used, or not shown with what the client asked for.
	ErrCodeInvalid = errors.New("the authorization code is invalid")
	// ErrResourceInvalid is a resource that is not this hub's MCP server
	// (RFC 8707).
	ErrResourceInvalid = errors.New("the resource is not this hub's MCP server")
)

// OAuthError is a mistake in an authorization request that goes back to
// the client, named as in RFC 6749, section 4.1.2.1.
type OAuthError struct {
	Code        string
	Description string
}

func (e *OAuthError) Error() string { return e.Code + ": " + e.Description }

// OAuthService lets AI agents in with OAuth 2.1, as the MCP authorization
// spec asks. An MCP client registers itself (RFC 7591), sends the person
// here with a PKCE challenge (RFC 7636), the person approves on the same
// page as `moin login`, and the client trades the code for the tokens of
// the new connection. The connection is like any other: the same rights,
// refresh and removal. Its tokens work only at /mcp of this hub (RFC 8707).
type OAuthService struct {
	oauth       *repository.OAuth
	connections *ConnectionService
	passkeys    *PasskeyService
	issuer      string
	now         func() time.Time
}

// NewOAuthService runs OAuth for the hub at appURL, which is also its
// issuer.
func NewOAuthService(oauth *repository.OAuth, connections *ConnectionService, passkeys *PasskeyService, appURL string) *OAuthService {
	return &OAuthService{oauth: oauth, connections: connections, passkeys: passkeys, issuer: strings.TrimRight(appURL, "/"), now: time.Now}
}

// Issuer names this hub as an authorization server (RFC 8414): its URL.
func (s *OAuthService) Issuer() string { return s.issuer }

// Resource names the MCP server of this hub (RFC 8707, RFC 9728): the URL
// of /mcp.
func (s *OAuthService) Resource() string { return s.issuer + MCPPath }

// IsResource reports whether resource names the MCP server of this hub.
// Scheme and host may be in any case, and a trailing slash does not count.
func (s *OAuthService) IsResource(resource string) bool {
	given, err := url.Parse(resource)
	if err != nil || given.RawQuery != "" || strings.Contains(resource, "#") {
		return false
	}
	want, err := url.Parse(s.Resource())
	if err != nil {
		return false
	}
	return strings.EqualFold(given.Scheme, want.Scheme) && strings.EqualFold(given.Host, want.Host) && strings.TrimSuffix(given.Path, "/") == want.Path
}

// NewClient is what an MCP client sends when it registers: its name and
// where it may send the person back.
type NewClient struct {
	ClientName   string
	RedirectURIs []string
}

// Register stores a client that registered itself. It keeps only the
// redirect URIs this hub sends people to; RFC 7591 lets it drop the
// others, and a client such as Cursor registers a scheme of its own next
// to localhost. Without one left it fails with ErrRedirectURIs. Nothing
// about the client is checked or trusted: its name is its own word.
func (s *OAuthService) Register(input NewClient) (*model.OAuthClient, error) {
	now := s.now().UTC()
	if err := s.oauth.DeleteIdleClients(now.Add(-unusedClientLifetime), now.Add(-model.ConnectionIdle)); err != nil {
		slog.Warn("failed to delete idle oauth clients", "error", err)
	}
	keep := []string{}
	for _, uri := range input.RedirectURIs {
		if len(keep) == maxRedirectURIs {
			break
		}
		if validRedirectURI(uri) && !slices.Contains(keep, uri) {
			keep = append(keep, uri)
		}
	}
	if len(keep) == 0 {
		return nil, ErrRedirectURIs
	}
	client := model.OAuthClient{
		ID:           uuid.New().String(),
		ClientName:   CleanClientName(input.ClientName),
		RedirectURIs: strings.Join(keep, "\n"),
		CreatedAt:    now,
	}
	if err := s.oauth.CreateClient(client); err != nil {
		return nil, err
	}
	return &client, nil
}

// AuthorizationRequest is what a client asks at the authorization
// endpoint, with the names of RFC 6749 (4.1.1), RFC 7636 and RFC 8707.
type AuthorizationRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
}

// ParseAuthorizationRequest reads a request from its query or form.
func ParseAuthorizationRequest(values url.Values) AuthorizationRequest {
	return AuthorizationRequest{
		ResponseType:        values.Get("response_type"),
		ClientID:            values.Get("client_id"),
		RedirectURI:         values.Get("redirect_uri"),
		State:               values.Get("state"),
		CodeChallenge:       values.Get("code_challenge"),
		CodeChallengeMethod: values.Get("code_challenge_method"),
		Resource:            values.Get("resource"),
	}
}

// Values is the request as a query again, for the approval form and to
// come back to after signing in.
func (r AuthorizationRequest) Values() url.Values {
	values := url.Values{}
	for name, value := range map[string]string{
		"response_type":         r.ResponseType,
		"client_id":             r.ClientID,
		"redirect_uri":          r.RedirectURI,
		"state":                 r.State,
		"code_challenge":        r.CodeChallenge,
		"code_challenge_method": r.CodeChallengeMethod,
		"resource":              r.Resource,
	} {
		if value != "" {
			values.Set(name, value)
		}
	}
	return values
}

// Authorization is a request whose client and redirect URI are good, so
// its answer can go back to the client.
type Authorization struct {
	Request AuthorizationRequest
	Client  *model.OAuthClient
}

// RedirectHost is the host the person goes back to, for the approval page.
func (a Authorization) RedirectHost() string {
	parsed, err := url.Parse(a.Request.RedirectURI)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// Local reports whether the client goes back to this computer (localhost):
// then only its own word says which app it is.
func (a Authorization) Local() bool { return isLoopback(a.RedirectHost()) }

// Check checks an authorization request. ErrOAuthClient and
// ErrOAuthRedirect mean the answer cannot go back: the page says so
// itself. An *OAuthError goes back to the client, with the authorization
// that says where.
func (s *OAuthService) Check(request AuthorizationRequest) (*Authorization, error) {
	if request.ClientID == "" {
		return nil, ErrOAuthClient
	}
	client, err := s.oauth.Client(request.ClientID)
	if errors.Is(err, repository.ErrOAuthClientNotFound) {
		return nil, ErrOAuthClient
	}
	if err != nil {
		return nil, err
	}
	if !redirectMatches(client.Redirects(), request.RedirectURI) {
		return nil, ErrOAuthRedirect
	}
	authorization := &Authorization{Request: request, Client: client}
	switch {
	case request.ResponseType != "code":
		return authorization, &OAuthError{"unsupported_response_type", "Only the authorization code flow works: response_type=code."}
	case request.CodeChallengeMethod != "S256":
		return authorization, &OAuthError{"invalid_request", "Send a PKCE code challenge with code_challenge_method=S256."}
	case !validVerifier(request.CodeChallenge):
		return authorization, &OAuthError{"invalid_request", "The code challenge must be 43 to 128 letters, digits and -._~."}
	case request.Resource != "" && !s.IsResource(request.Resource):
		return authorization, &OAuthError{"invalid_target", "The resource must be " + s.Resource() + "."}
	}
	return authorization, nil
}

// OAuthApproval is a person letting a client in: in which workspace, with
// which rights, and the proof they are there now.
type OAuthApproval struct {
	Authorization *Authorization
	WorkspaceID   string
	Rights        model.Rights
	Presence      Presence
	Source        Source
}

// Approve makes the connection, once the person proved they are there now,
// and returns where to send them: back to the client with a code for its
// tokens.
func (s *OAuthService) Approve(approval OAuthApproval) (string, *model.Connection, error) {
	s.endUntraded()
	if err := s.connections.confirmApproval(s.passkeys, approval.Presence, approval.WorkspaceID, approval.Rights); err != nil {
		return "", nil, err
	}
	authorization := approval.Authorization
	connection, err := s.connections.Create(NewConnection{
		UserID:      approval.Presence.Session.UserID,
		WorkspaceID: approval.WorkspaceID,
		ClientName:  authorization.Client.ClientName,
		Rights:      approval.Rights,
		ClientID:    authorization.Client.ID,
		Resource:    MCPPath,
		Source:      approval.Source,
	})
	if err != nil {
		return "", nil, err
	}
	raw, _, err := newToken("")
	if err != nil {
		return "", nil, err
	}
	now := s.now().UTC()
	err = s.oauth.CreateCode(model.OAuthCode{
		ID:            uuid.New().String(),
		CodeHash:      hashToken(raw),
		ConnectionID:  connection.ID,
		RedirectURI:   authorization.Request.RedirectURI,
		CodeChallenge: authorization.Request.CodeChallenge,
		ExpiresAt:     now.Add(model.OAuthCodeLifetime),
		CreatedAt:     now,
	})
	if err != nil {
		s.connections.end(*connection, Source{Via: ViaSystem}, "the authorization code could not be saved")
		return "", nil, err
	}
	if err := s.oauth.ClientUsed(authorization.Client.ID, now); err != nil {
		slog.Warn("failed to record an oauth client's use", "error", err, "client_id", authorization.Client.ID)
	}
	return s.redirect(authorization, url.Values{"code": {raw}}), connection, nil
}

// Deny is where to send the person who said no: back to the client, which
// hears access_denied.
func (s *OAuthService) Deny(authorization *Authorization) string {
	return s.Refuse(authorization, &OAuthError{"access_denied", "The person did not allow it."})
}

// Refuse is where to send the person when the request was wrong: back to
// the client, with the error.
func (s *OAuthService) Refuse(authorization *Authorization, refusal *OAuthError) string {
	return s.redirect(authorization, url.Values{"error": {refusal.Code}, "error_description": {refusal.Description}})
}

// redirect is the client's redirect URI with the answer, the state it sent
// and this hub as the issuer (RFC 9207), so a client that talks to several
// hubs knows which one answered.
func (s *OAuthService) redirect(authorization *Authorization, answer url.Values) string {
	target, err := url.Parse(authorization.Request.RedirectURI)
	if err != nil {
		return ""
	}
	query := target.Query()
	for name, values := range answer {
		query[name] = values
	}
	if authorization.Request.State != "" {
		query.Set("state", authorization.Request.State)
	}
	query.Set("iss", s.issuer)
	target.RawQuery = query.Encode()
	return target.String()
}

// CodeExchange is a client trading its code for tokens (RFC 6749 4.1.3,
// RFC 7636 4.5, RFC 8707).
type CodeExchange struct {
	Code        string
	Verifier    string
	RedirectURI string
	ClientID    string
	Resource    string
}

// Exchange trades an authorization code for the first tokens of its
// connection. A code works for one try: the first marks it used, whatever
// comes of it. A mistake then ends the connection, since a client that
// does it right makes none, and a code shown a second time ends the
// connection with the tokens the first try got (OAuth 2.1, 4.1.3).
func (s *OAuthService) Exchange(exchange CodeExchange) (Tokens, error) {
	s.endUntraded()
	if exchange.Code == "" {
		return Tokens{}, ErrCodeInvalid
	}
	code, err := s.oauth.Code(hashToken(exchange.Code))
	if errors.Is(err, repository.ErrOAuthCodeNotFound) {
		return Tokens{}, ErrCodeInvalid
	}
	if err != nil {
		return Tokens{}, err
	}
	connection, err := s.connections.connections.ByID(code.ConnectionID)
	if errors.Is(err, repository.ErrConnectionNotFound) {
		return Tokens{}, ErrCodeInvalid
	}
	if err != nil {
		return Tokens{}, err
	}
	now := s.now().UTC()
	err = s.oauth.UseCode(code.ID, now)
	if errors.Is(err, repository.ErrOAuthCodeUsed) {
		s.connections.end(*connection, Source{Via: ViaSystem}, "the authorization code was used twice")
		return Tokens{}, ErrCodeInvalid
	}
	if err != nil {
		return Tokens{}, err
	}
	refused, reason := ErrCodeInvalid, ""
	switch {
	case !code.ExpiresAt.After(now):
		reason = "the authorization code expired"
	case connection.ClientID != exchange.ClientID:
		reason = "the authorization code came from another client"
	case code.RedirectURI != exchange.RedirectURI:
		reason = "the authorization code came with another redirect URI"
	case !pkceMatches(exchange.Verifier, code.CodeChallenge):
		reason = "the PKCE code verifier was wrong"
	case exchange.Resource != "" && !s.IsResource(exchange.Resource):
		refused, reason = ErrResourceInvalid, "the authorization code came for another resource"
	}
	if reason != "" {
		s.connections.end(*connection, Source{Via: ViaSystem}, reason)
		return Tokens{}, refused
	}
	return s.connections.Issue(connection)
}

// endUntraded ends the connections whose code nobody traded in time: the
// client gave up, and a connection without tokens should not stay.
func (s *OAuthService) endUntraded() {
	codes, err := s.oauth.ExpiredUnusedCodes(s.now().UTC())
	if err != nil {
		slog.Warn("failed to list untraded oauth codes", "error", err)
		return
	}
	for _, code := range codes {
		connection, err := s.connections.connections.ByID(code.ConnectionID)
		if err != nil {
			continue
		}
		s.connections.end(*connection, Source{Via: ViaSystem}, "the authorization code was never used")
	}
}

// pkceMatches checks a PKCE code verifier against the S256 challenge:
// BASE64URL(SHA256(verifier)) (RFC 7636, section 4.6).
func pkceMatches(verifier, challenge string) bool {
	if !validVerifier(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// validVerifier reports whether value is 43 to 128 unreserved characters,
// as a PKCE verifier and an S256 challenge are (RFC 7636, section 4.1).
func validVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.', r == '_', r == '~':
		default:
			return false
		}
	}
	return true
}

// validRedirectURI reports whether this hub sends people to uri: https,
// or http on this computer for an app that runs there, and never with a
// fragment or a user (OAuth 2.1, sections 2.3 and 8.4).
func validRedirectURI(uri string) bool {
	if uri == "" || len(uri) > maxRedirectURILength || strings.Contains(uri, "#") {
		return false
	}
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		return isLoopback(parsed.Hostname())
	}
	return false
}

// redirectMatches reports whether uri is one of the registered URIs,
// letter for letter. On this computer only the port may differ: an app
// gets a free port each time it starts (RFC 8252, section 7.3; OAuth 2.1,
// section 8.4.2).
func redirectMatches(registered []string, uri string) bool {
	if !validRedirectURI(uri) {
		return false
	}
	for _, candidate := range registered {
		if candidate == uri || sameLoopbackButPort(candidate, uri) {
			return true
		}
	}
	return false
}

func sameLoopbackButPort(registered, uri string) bool {
	a, errA := url.Parse(registered)
	b, errB := url.Parse(uri)
	if errA != nil || errB != nil || a.Scheme != "http" || b.Scheme != "http" || !isLoopback(a.Hostname()) {
		return false
	}
	return a.Hostname() == b.Hostname() && a.EscapedPath() == b.EscapedPath() && a.RawQuery == b.RawQuery
}

// isLoopback reports whether host is this computer.
func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
