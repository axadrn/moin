package daemon

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// Every server runs one Traefik container for all apps. It takes 80 and
// 443, gets certificates from Let's Encrypt and sends HTTP to HTTPS. Apps
// join its network and describe their routes with labels on the main
// service.
const proxyImage = "traefik:v3.6"

var domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// route settles the domains of a deploy: x-moin wins for every field it
// sets, the hub's settings fill the rest. Domains are the main
// service's; ServiceDomains those of the others; Instant its instant
// address.
func route(app string, detected protocol.Detected, domains *protocol.Domains) (protocol.Detected, error) {
	if domains == nil {
		domains = &protocol.Domains{}
	}
	config := detected.Config
	if config == nil {
		config = &protocol.Config{}
	}
	detected.Domains = mainDomains(detected.Service, domains, config)
	detected.ServiceDomains = otherServiceDomains(detected.Service, domains, config)
	detected.Canonical = domains.Canonical
	if config.Canonical != "" {
		detected.Canonical = config.Canonical
	}
	detected.Protected = domains.Protect != "" || config.Protect
	err := cleanDomains(detected)
	if err != nil {
		return detected, err
	}
	if detected.Canonical != "" && !slices.Contains(detected.Domains, detected.Canonical) {
		return detected, fmt.Errorf("canonical domain %s is not one of the domains", detected.Canonical)
	}
	if config.Protect && domains.Protect == "" {
		return detected, errors.New("x-moin.protect is on but no password is set: set one with moin protect")
	}
	provider, err := instantProvider(domains, config)
	if err != nil {
		return detected, err
	}
	detected.Instant = protocol.InstantHost(app, domains.InstantIP, provider)
	return detected, nil
}

// mainDomains are the domains of the main service.
func mainDomains(mainName string, domains *protocol.Domains, config *protocol.Config) []string {
	if len(config.Domains) > 0 {
		return slices.Clone(config.Domains)
	}
	// The map form of x-moin domains may name the main service too.
	named, ok := config.ServiceDomains[mainName]
	if ok {
		return slices.Clone(named)
	}
	return slices.Clone(domains.Domains)
}

// otherServiceDomains are the domains of the services other than the main
// one, nil when there are none.
func otherServiceDomains(mainName string, domains *protocol.Domains, config *protocol.Config) map[string][]string {
	byService := map[string][]string{}
	for service, list := range domains.Services {
		if service != mainName {
			byService[service] = slices.Clone(list)
		}
	}
	for service, list := range config.ServiceDomains {
		if service != mainName {
			byService[service] = slices.Clone(list)
		}
	}
	for service, list := range byService {
		if len(list) == 0 {
			delete(byService, service)
		}
	}
	if len(byService) == 0 {
		return nil
	}
	return byService
}

// cleanDomains checks every domain of a deploy and writes it in lower case.
// A domain reaches one service.
func cleanDomains(detected protocol.Detected) error {
	seen := map[string]bool{}
	err := cleanDomainList(detected.Domains, seen)
	if err != nil {
		return err
	}
	for _, list := range detected.ServiceDomains {
		err := cleanDomainList(list, seen)
		if err != nil {
			return err
		}
	}
	return nil
}

// cleanDomainList cleans one list in place. seen holds the domains so far.
func cleanDomainList(list []string, seen map[string]bool) error {
	for index, domain := range list {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if !domainPattern.MatchString(domain) {
			return fmt.Errorf("%q is not a domain name", domain)
		}
		if seen[domain] {
			return fmt.Errorf("%s is on two services: a domain reaches one", domain)
		}
		seen[domain] = true
		list[index] = domain
	}
	return nil
}

// instantProvider is the provider of the instant address: x-moin wins over
// the hub's setting, and off means none.
func instantProvider(domains *protocol.Domains, config *protocol.Config) (string, error) {
	switch config.Instant {
	case "":
		return domains.Instant, nil
	case "off":
		return "", nil
	}
	if !slices.Contains(protocol.InstantProviders, config.Instant) {
		return "", fmt.Errorf("x-moin.instant is %q: use off, %s", config.Instant, strings.Join(protocol.InstantProviders, ", "))
	}
	return config.Instant, nil
}

func protectHash(domains *protocol.Domains) string {
	if domains == nil {
		return ""
	}
	return domains.Protect
}

func instantProtectHash(domains *protocol.Domains) string {
	if domains == nil {
		return ""
	}
	return domains.InstantProtect
}

// routes says how the proxy reaches one service of an app.
type routes struct {
	app string
	// router names its router, such as moin-my-app for the main service.
	router    string
	domains   []string
	canonical string
	port      int
	// protect is the password in front of the domains, "" for none.
	protect string
	// instant is the instant address, "" for none, on a router of its own.
	instant string
	// instantProtect is the password in front of the instant address.
	instantProtect string
}

// addRouters puts a service of an app on the proxy network and labels its
// routes.
func addRouters(config, service map[string]any, routes routes) {
	labels := map[string]any{
		"traefik.enable":         "true",
		"traefik.docker.network": proxyNetwork,
		"traefik.http.services." + routes.router + ".loadbalancer.server.port": fmt.Sprint(routes.port),
	}
	if len(routes.domains) > 0 {
		addDomainLabels(labels, routes)
	}
	if routes.instant != "" {
		addInstantLabels(labels, routes)
	}
	mergeLabels(service, labels)
	joinProxyNetwork(config, service, routes.app)
}

// addDomainLabels labels every domain over HTTPS, the other domains
// redirecting to the canonical one, and basic auth when a password is set.
func addDomainLabels(labels map[string]any, routes routes) {
	name := routes.router
	hosts := make([]string, 0, len(routes.domains))
	for _, domain := range routes.domains {
		hosts = append(hosts, "Host(`"+domain+"`)")
	}
	rule := strings.Join(hosts, " || ")
	labels["traefik.http.routers."+name+".rule"] = rule
	labels["traefik.http.routers."+name+".entrypoints"] = "websecure"
	labels["traefik.http.routers."+name+".service"] = name
	middlewares := addDomainMiddlewares(labels, routes)
	if len(middlewares) > 0 {
		labels["traefik.http.routers."+name+".middlewares"] = strings.Join(middlewares, ",")
	}
	// The same routes for traffic from a Cloudflare Tunnel, which
	// arrives as HTTP on an entrypoint no port publishes.
	labels["traefik.http.routers."+name+"-tunnel.rule"] = rule
	labels["traefik.http.routers."+name+"-tunnel.entrypoints"] = tunnelEntrypoint
	labels["traefik.http.routers."+name+"-tunnel.service"] = name
	if len(middlewares) > 0 {
		labels["traefik.http.routers."+name+"-tunnel.middlewares"] = strings.Join(middlewares, ",")
	}
}

// addDomainMiddlewares labels the canonical redirect and the password, when
// the service has them, and returns their names.
func addDomainMiddlewares(labels map[string]any, routes routes) []string {
	name := routes.router
	middlewares := []string{}
	others := []string{}
	for _, domain := range routes.domains {
		if routes.canonical != "" && domain != routes.canonical {
			others = append(others, regexp.QuoteMeta(domain))
		}
	}
	if len(others) > 0 {
		labels["traefik.http.middlewares."+name+"-canonical.redirectregex.regex"] = escape("^https?://(" + strings.Join(others, "|") + ")(:[0-9]+)?(.*)")
		labels["traefik.http.middlewares."+name+"-canonical.redirectregex.replacement"] = escape("https://" + routes.canonical + "${3}")
		labels["traefik.http.middlewares."+name+"-canonical.redirectregex.permanent"] = "true"
		middlewares = append(middlewares, name+"-canonical")
	}
	if routes.protect != "" {
		labels["traefik.http.middlewares."+name+"-auth.basicauth.users"] = escape(routes.protect)
		middlewares = append(middlewares, name+"-auth")
	}
	return middlewares
}

// addInstantLabels labels the instant address. It has a router of its own:
// no canonical redirect, and its own password, so own domains can stay
// public.
func addInstantLabels(labels map[string]any, routes routes) {
	router := routes.router + "-instant"
	labels["traefik.http.routers."+router+".rule"] = "Host(`" + routes.instant + "`)"
	labels["traefik.http.routers."+router+".entrypoints"] = "websecure"
	labels["traefik.http.routers."+router+".service"] = routes.router
	if routes.instantProtect != "" {
		labels["traefik.http.middlewares."+router+"-auth.basicauth.users"] = escape(routes.instantProtect)
		labels["traefik.http.routers."+router+".middlewares"] = router + "-auth"
	}
}

// mergeLabels adds labels to the ones a service already has.
func mergeLabels(service, labels map[string]any) {
	existing, _ := service["labels"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	for key, value := range labels {
		existing[key] = value
	}
	service["labels"] = existing
}

// joinProxyNetwork puts a service on the proxy's network, next to its own.
func joinProxyNetwork(config, service map[string]any, app string) {
	networks, _ := service["networks"].(map[string]any)
	if networks == nil {
		networks = map[string]any{"default": nil}
	}
	networks[proxyNetwork] = nil
	service["networks"] = networks
	top, _ := config["networks"].(map[string]any)
	if top == nil {
		top = map[string]any{"default": map[string]any{"name": project(app) + "_default"}}
	}
	top[proxyNetwork] = map[string]any{"name": proxyNetwork, "external": true}
	config["networks"] = top
}

// serviceRouter names the router of a service other than the main one.
func serviceRouter(app, service string) string { return project(app) + "-svc-" + service }

// ensureProxy starts the shared Traefik container when it does not run,
// runs an older image or another mode. In direct mode it takes 80 and 443;
// in tunnel mode it publishes no port and cloudflared reaches it on the
// tunnel entrypoint. MOIN_HTTP_PORT and MOIN_HTTPS_PORT move it off 80 and
// 443 for development.
func (a *apps) ensureProxy(ctx context.Context, logf func(string, ...any)) error {
	mode := "direct"
	if a.proxyMode != nil {
		mode = a.proxyMode()
	}
	if _, err := a.output(ctx, "docker", "network", "inspect", proxyNetwork); err != nil {
		_, err = a.output(ctx, "docker", "network", "create", proxyNetwork)
		if err != nil {
			return err
		}
	}
	// A proxy on an older image or with other flags is replaced, so proxy
	// updates reach every server. A label holds a hash of the flags.
	flags := proxyFlags(mode)
	setup := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(flags, "\n"))))[:12]
	running, err := a.output(ctx, "docker", "inspect", "-f", "{{.State.Running}} {{.Config.Image}} {{index .Config.Labels \""+labelMode+"\"}} {{index .Config.Labels \""+labelSetup+"\"}}", proxyName)
	if err == nil && strings.TrimSpace(running) == "true "+proxyImage+" "+mode+" "+setup {
		return nil
	}
	if err == nil {
		_, _ = a.output(ctx, "docker", "rm", "-f", proxyName)
	}
	data := filepath.Join(filepath.Dir(a.dir), "traefik")
	err = os.MkdirAll(data, 0o700)
	if err != nil {
		return err
	}
	args := []string{"run", "-d", "--name", proxyName, "--restart", "unless-stopped",
		"--network", proxyNetwork, "--label", labelMode + "=" + mode, "--label", labelSetup + "=" + setup}
	if mode == "direct" {
		httpPort, httpsPort := envOr("HTTP_PORT", "80"), envOr("HTTPS_PORT", "443")
		logf("starting the proxy on ports %s and %s", httpPort, httpsPort)
		args = append(args, "-p", httpPort+":80", "-p", httpsPort+":443")
	} else {
		logf("starting the proxy behind the Cloudflare Tunnel, with no open port")
	}
	args = append(args,
		"-v", "/var/run/docker.sock:/var/run/docker.sock:ro",
		"-v", data+":/data",
		proxyImage,
	)
	args = append(args, flags...)
	_, err = a.output(ctx, "docker", args...)
	return err
}

// proxyFlags configures Traefik for the mode. In direct mode the HTTPS
// entrypoint gets its certificates from Let's Encrypt. In tunnel mode
// Cloudflare serves HTTPS and no port is open, so Let's Encrypt could never
// reach the server: nothing asks it.
func proxyFlags(mode string) []string {
	flags := []string{
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.docker.network=" + proxyNetwork,
		"--entrypoints." + tunnelEntrypoint + ".address=:" + tunnelPort,
		"--entrypoints.web.address=:80",
		"--entrypoints.web.http.redirections.entrypoint.to=websecure",
		"--entrypoints.web.http.redirections.entrypoint.scheme=https",
		"--entrypoints.websecure.address=:443",
	}
	if mode == "tunnel" {
		return flags
	}
	return append(flags,
		"--entrypoints.websecure.http.tls.certresolver=le",
		"--certificatesresolvers.le.acme.httpchallenge=true",
		"--certificatesresolvers.le.acme.httpchallenge.entrypoint=web",
		"--certificatesresolvers.le.acme.storage=/data/acme.json",
	)
}

func envOr(key, fallback string) string {
	if value := build.Getenv(key); value != "" {
		return value
	}
	return fallback
}
