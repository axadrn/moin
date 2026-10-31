package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/axadrn/ops/protocol"
)

// Every server runs one Traefik container for all apps. It takes 80 and
// 443, gets certificates from Let's Encrypt and sends HTTP to HTTPS. Apps
// join its network and describe their routes with labels on the main
// service.
const (
	proxyName    = "ops-traefik"
	proxyNetwork = "ops-proxy"
	proxyImage   = "traefik:v3.6"
)

var domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// route settles the routing of a release: x-ops wins for every field it
// sets, the control plane's settings fill the rest.
func route(detected protocol.Detected, routing *protocol.Routing) (protocol.Detected, error) {
	if routing == nil {
		routing = &protocol.Routing{}
	}
	config := detected.Config
	if config == nil {
		config = &protocol.Config{}
	}
	detected.Domains = routing.Domains
	if len(config.Domains) > 0 {
		detected.Domains = config.Domains
	}
	detected.Canonical = routing.Canonical
	if config.Canonical != "" {
		detected.Canonical = config.Canonical
	}
	detected.Protected = routing.Protect != "" || config.Protect
	for index, domain := range detected.Domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if !domainPattern.MatchString(domain) {
			return detected, fmt.Errorf("%q is not a domain name", domain)
		}
		detected.Domains[index] = domain
	}
	if detected.Canonical != "" && !slices.Contains(detected.Domains, detected.Canonical) {
		return detected, fmt.Errorf("canonical domain %s is not one of the domains", detected.Canonical)
	}
	if config.Protect && routing.Protect == "" {
		return detected, errors.New("x-ops.protect is on but no password is set: set one with ops protect")
	}
	return detected, nil
}

func protectHash(routing *protocol.Routing) string {
	if routing == nil {
		return ""
	}
	return routing.Protect
}

// addRouting puts the main service on the proxy network and labels its
// routes: every domain over HTTPS, the other domains redirecting to the
// canonical one, and basic auth when a password is set.
func addRouting(config, service map[string]any, app string, detected protocol.Detected, protect string) {
	name := project(app)
	hosts := make([]string, 0, len(detected.Domains))
	for _, domain := range detected.Domains {
		hosts = append(hosts, "Host(`"+domain+"`)")
	}
	labels := map[string]any{
		"traefik.enable":                                              "true",
		"traefik.docker.network":                                      proxyNetwork,
		"traefik.http.routers." + name + ".rule":                      strings.Join(hosts, " || "),
		"traefik.http.routers." + name + ".entrypoints":               "websecure",
		"traefik.http.routers." + name + ".tls.certresolver":          "le",
		"traefik.http.services." + name + ".loadbalancer.server.port": fmt.Sprint(detected.Port),
	}
	middlewares := []string{}
	others := []string{}
	for _, domain := range detected.Domains {
		if detected.Canonical != "" && domain != detected.Canonical {
			others = append(others, regexp.QuoteMeta(domain))
		}
	}
	if len(others) > 0 {
		labels["traefik.http.middlewares."+name+"-canonical.redirectregex.regex"] = escape("^https?://(" + strings.Join(others, "|") + ")(:[0-9]+)?(.*)")
		labels["traefik.http.middlewares."+name+"-canonical.redirectregex.replacement"] = escape("https://" + detected.Canonical + "${3}")
		labels["traefik.http.middlewares."+name+"-canonical.redirectregex.permanent"] = "true"
		middlewares = append(middlewares, name+"-canonical")
	}
	if protect != "" {
		labels["traefik.http.middlewares."+name+"-auth.basicauth.users"] = escape(protect)
		middlewares = append(middlewares, name+"-auth")
	}
	if len(middlewares) > 0 {
		labels["traefik.http.routers."+name+".middlewares"] = strings.Join(middlewares, ",")
	}
	existing, _ := service["labels"].(map[string]any)
	for key, value := range labels {
		if existing == nil {
			existing = map[string]any{}
		}
		existing[key] = value
	}
	service["labels"] = existing

	networks, _ := service["networks"].(map[string]any)
	if networks == nil {
		networks = map[string]any{"default": nil}
	}
	networks[proxyNetwork] = nil
	service["networks"] = networks
	top, _ := config["networks"].(map[string]any)
	if top == nil {
		top = map[string]any{"default": map[string]any{"name": name + "_default"}}
	}
	top[proxyNetwork] = map[string]any{"name": proxyNetwork, "external": true}
	config["networks"] = top
}

// ensureProxy starts the shared Traefik container when it does not run, or
// runs an older image.
// OPS_HTTP_PORT and OPS_HTTPS_PORT move it off 80 and 443 for development.
func (a *apps) ensureProxy(ctx context.Context, logf func(string, ...any)) error {
	if _, err := a.output(ctx, "docker", "network", "inspect", proxyNetwork); err != nil {
		_, err = a.output(ctx, "docker", "network", "create", proxyNetwork)
		if err != nil {
			return err
		}
	}
	// A proxy on an older image is replaced, so proxy updates reach every server.
	running, err := a.output(ctx, "docker", "inspect", "-f", "{{.State.Running}} {{.Config.Image}}", proxyName)
	if err == nil && strings.TrimSpace(running) == "true "+proxyImage {
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
	httpPort, httpsPort := envOr("OPS_HTTP_PORT", "80"), envOr("OPS_HTTPS_PORT", "443")
	logf("starting the proxy on ports %s and %s", httpPort, httpsPort)
	_, err = a.output(ctx, "docker", "run", "-d", "--name", proxyName, "--restart", "unless-stopped",
		"--network", proxyNetwork,
		"-p", httpPort+":80", "-p", httpsPort+":443",
		"-v", "/var/run/docker.sock:/var/run/docker.sock:ro",
		"-v", data+":/data",
		proxyImage,
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.docker.network="+proxyNetwork,
		"--entrypoints.web.address=:80",
		"--entrypoints.web.http.redirections.entrypoint.to=websecure",
		"--entrypoints.web.http.redirections.entrypoint.scheme=https",
		"--entrypoints.websecure.address=:443",
		"--certificatesresolvers.le.acme.httpchallenge=true",
		"--certificatesresolvers.le.acme.httpchallenge.entrypoint=web",
		"--certificatesresolvers.le.acme.storage=/data/acme.json",
	)
	return err
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
