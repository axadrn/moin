package service

import (
	"crypto/rand"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrService          = errors.New("a service name is lowercase letters, digits, dashes, dots or underscores")
	ErrMainService      = errors.New("this is the app's main service: set its domains without --service")
	ErrServiceCanonical = errors.New("canonical is for the main service only")
	ErrInstantProvider  = errors.New("the instant address provider is sslip.io, nip.io or off")
)

// servicePattern is a compose service name.
var servicePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,62}$`)

// InstantUser is the username in front of an instant address that has no
// password of the app.
const InstantUser = build.Name

// setServiceDomains replaces the domains of one of the app's services other
// than the main one. An empty list removes the service's domains.
func (s *AppService) setServiceDomains(actor Actor, idOrName, service string, domains []string) (changed bool, err error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return false, err
	}
	service = strings.TrimSpace(service)
	if !servicePattern.MatchString(service) {
		return false, ErrService
	}
	if main := s.mainService(app); main != "" && main == service {
		return false, ErrMainService
	}
	if len(s.composeServiceDomains(app)[service]) > 0 {
		return false, ErrComposeDomains
	}
	clean, err := cleanDomains(domains)
	if err != nil {
		return false, err
	}
	saved, _ := app.DomainsOf(service)
	if sameDomains(clean, saved) {
		return false, nil
	}
	before := strings.Join(saved, ", ")
	err = s.apps.SetDomains(app.ID, service, clean, "")
	if err != nil {
		return false, err
	}
	s.servers.record(actor, "app.domains_set", "app", app.Name, map[string]string{"service": service, "from": before, "domains": strings.Join(clean, ", ")})
	s.notify(actor.WorkspaceID)
	return true, nil
}

// cleanDomains lowercases the domains, drops empty ones and repeats, and
// refuses one that is no host name.
func cleanDomains(domains []string) ([]string, error) {
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
	return clean, nil
}

// sameDomains says two lists hold the same domains, in any order: the
// order of the domains of a service means nothing.
func sameDomains(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// mainService is the service that gets the traffic in the live deploy, ""
// before the first deploy.
func (s *AppService) mainService(app *model.App) string {
	if app.LiveNumber == 0 {
		return ""
	}
	if detected := s.liveDetected(app); detected != nil {
		return detected.Service
	}
	return ""
}

// Instant is the instant address of an app: Host and URL when it has one,
// else Reason says why not. Open is true when no password guards it; then
// User is empty. moin never shows the password again after it made it
// (NewInstantPassword).
type Instant struct {
	Provider string
	Host     string
	URL      string
	Reason   string
	Open     bool
	User     string
	// OwnPassword says the app's own password guards it, the one of its
	// domains; moin shows no password then.
	OwnPassword bool
}

// Instant reports the app's instant address.
func (s *AppService) Instant(workspaceID, idOrName string) (Instant, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return Instant{}, err
	}
	server, err := s.servers.servers.ByIDOrNameForWorkspace(app.ServerID, workspaceID)
	if err != nil {
		return Instant{}, err
	}
	provider := app.Instant
	if inFile := s.FileConfig(app).Instant; inFile != "" {
		provider = inFile
	}
	instant := Instant{Provider: provider, Open: app.InstantOpen}
	host := app.InstantHost
	if app.LiveNumber == 0 {
		host = s.instantHost(app, server)
	}
	switch {
	case host != "":
		instant.Host, instant.URL = host, "https://"+host
	case provider == "off" || provider == "":
		instant.Reason = "Off."
	case server.Tunnel:
		instant.Reason = "The server is behind a Cloudflare Tunnel: an instant address needs ports 80 and 443 open."
	default:
		if ipv4, _ := ServerAddresses(*server); ipv4 == "" {
			instant.Reason = "The server has no public IPv4 the address could name."
		} else {
			instant.Reason = "x-moin.instant in compose.yml turns it off."
		}
	}
	if !instant.Open {
		if app.Protect != "" {
			instant.OwnPassword = true
			instant.User, _, _ = strings.Cut(app.Protect, ":")
		} else {
			instant.User = InstantUser
		}
	}
	return instant, nil
}

// SetInstant picks the provider of the app's instant address, or off, and
// whether it is open to everyone; the app takes it on its next restart. An
// empty provider keeps the one it has; open nil keeps the password as it is.
func (s *AppService) SetInstant(actor Actor, idOrName, provider string, open *bool) (changed bool, err error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return false, err
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" && s.FileConfig(app).Instant != "" {
		return false, ErrInstantInFile
	}
	if provider == "" {
		provider = app.Instant
	}
	if provider != "off" && !slices.Contains(protocol.InstantProviders, provider) {
		return false, ErrInstantProvider
	}
	isOpen := app.InstantOpen
	if open != nil {
		isOpen = *open
	}
	if provider == app.Instant && isOpen == app.InstantOpen {
		return false, nil
	}
	err = s.apps.SetInstant(app.ID, provider, isOpen)
	if err != nil {
		return false, err
	}
	details := map[string]string{"from": app.Instant, "provider": provider}
	if isOpen != app.InstantOpen {
		details["open"] = boolWord(isOpen)
	}
	app.Instant, app.InstantOpen = provider, isOpen
	s.servers.record(actor, "app.instant_set", "app", app.Name, details)
	s.notify(actor.WorkspaceID)
	return true, nil
}

func boolWord(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// instantHost is the instant address the app gets on the server, from its
// settings: "" when off, behind a tunnel, or without a public IPv4.
func (s *AppService) instantHost(app *model.App, server *model.Server) string {
	if server == nil || server.Tunnel {
		return ""
	}
	ipv4, _ := ServerAddresses(*server)
	return protocol.InstantHost(app.Folder, ipv4, app.Instant)
}

// fillInstant fills the instant address of a deploy: the provider, the
// server's IPv4 and the password in front of it.
func (s *AppService) fillInstant(app *model.App, domains *protocol.Domains) error {
	server, err := s.servers.servers.ByIDOrNameForWorkspace(app.ServerID, app.WorkspaceID)
	if err != nil {
		return err
	}
	// Also when it is off here: x-moin.instant in compose.yml may turn it on.
	if server.Tunnel {
		return nil
	}
	domains.Instant = app.Instant
	domains.InstantIP, _ = ServerAddresses(*server)
	if domains.InstantIP == "" {
		return nil
	}
	switch {
	case app.InstantOpen:
	case app.Protect != "":
		domains.InstantProtect = app.Protect
	default:
		// An app from before moin kept only the hash gets a password
		// nobody knows: New password shows one.
		if app.InstantPasswordHash == "" {
			if _, err := s.newInstantPassword(app); err != nil {
				return err
			}
		}
		domains.InstantProtect = InstantUser + ":" + app.InstantPasswordHash
	}
	// The address the app gets, until the deploy reports the one it serves.
	if host := protocol.InstantHost(app.Folder, domains.InstantIP, app.Instant); host != app.InstantHost && app.LiveNumber == 0 {
		_ = s.apps.SetInstantHost(app.ID, host)
	}
	return nil
}

// NewInstantPassword makes a new password for the app's instant address
// and returns it: this is the one time moin shows it, it keeps only its
// hash. The app takes it on its next restart.
func (s *AppService) NewInstantPassword(actor Actor, idOrName string) (string, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return "", err
	}
	password, err := s.newInstantPassword(app)
	if err != nil {
		return "", err
	}
	s.servers.record(actor, "app.instant_password", "app", app.Name, nil)
	s.notify(actor.WorkspaceID)
	return password, nil
}

// newInstantPassword makes a password for the app's instant address and
// keeps its bcrypt hash.
func (s *AppService) newInstantPassword(app *model.App) (string, error) {
	password := rand.Text()[:20]
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	if err := s.apps.SetInstantPasswordHash(app.ID, string(hash)); err != nil {
		return "", err
	}
	app.InstantPasswordHash = string(hash)
	return password, nil
}

// serviceDomains is the domains of the app's other services for a deploy.
func serviceDomains(app *model.App) map[string][]string {
	services := app.ServiceDomains()
	if len(services) == 0 {
		return nil
	}
	return services
}
