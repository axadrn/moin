package daemon

import (
	"context"
	"crypto/sha256"
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

// route settles the domains of a deploy from the app's settings on the
// server, the only place they are set. Domains are the main service's;
// ServiceDomains the other services', by service; Instant its instant
// address.
func route(app string, detected protocol.Detected, domains *protocol.Domains) (protocol.Detected, error) {
	if domains == nil {
		domains = &protocol.Domains{}
	}
	detected.Domains = slices.Clone(domains.Domains)
	detected.ServiceDomains = nil
	for service, list := range domains.Services {
		if service == detected.Service {
			detected.Domains = append(detected.Domains, list...)
			continue
		}
		if detected.ServiceDomains == nil {
			detected.ServiceDomains = map[string][]string{}
		}
		detected.ServiceDomains[service] = slices.Clone(list)
	}
	// A domain reaches one service.
	seen := map[string]bool{}
	if err := cleanDomains(detected.Domains, seen); err != nil {
		return detected, err
	}
	for _, list := range detected.ServiceDomains {
		if err := cleanDomains(list, seen); err != nil {
			return detected, err
		}
	}
	detected.Instant = protocol.InstantHost(app, domains.InstantIP, domains.Instant)
	return detected, nil
}

// cleanDomains checks the domains of a list and writes them in lower case,
// in place. seen holds the domains so far.
func cleanDomains(list []string, seen map[string]bool) error {
	for index, domain := range list {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if !domainPattern.MatchString(domain) {
			return fmt.Errorf("%q is not a domain name", domain)
		}
		if seen[domain] {
			return fmt.Errorf("%s is there twice", domain)
		}
		seen[domain] = true
		list[index] = domain
	}
	return nil
}

// routes says how the proxy reaches one service of an app.
type routes struct {
	app string
	// router names its router, such as moin-my-app for the main service.
	router  string
	domains []string
	port    int
	// instant is the instant address, "" for none, on a router of its own.
	instant string
	// usersFile is where the proxy reads the app's password, "" for none.
	// Every router of the app asks for it.
	usersFile string
}

// addRouters puts a service of an app on the proxy network and labels its
// routes.
func addRouters(config, service map[string]any, routes routes) {
	labels := map[string]any{
		"traefik.enable":         "true",
		"traefik.docker.network": proxyNetwork,
		"traefik.http.services." + routes.router + ".loadbalancer.server.port": fmt.Sprint(routes.port),
	}
	if routes.usersFile != "" {
		labels["traefik.http.middlewares."+routes.router+"-auth.basicauth.usersfile"] = routes.usersFile
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

// addDomainLabels labels every domain over HTTPS. When the app has both
// example.com and www.example.com, www redirects to example.com. An app
// with a password asks for it after that redirect.
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
	// The same routes for traffic from a Cloudflare Tunnel, which
	// arrives as HTTP on an entrypoint no port publishes.
	labels["traefik.http.routers."+name+"-tunnel.rule"] = rule
	labels["traefik.http.routers."+name+"-tunnel.entrypoints"] = tunnelEntrypoint
	labels["traefik.http.routers."+name+"-tunnel.service"] = name
	// Cloudflare says in X-Forwarded-Proto how the visitor came. One who
	// came over http goes to https, path and query kept.
	labels["traefik.http.middlewares."+name+"-https.redirectscheme.scheme"] = "https"
	labels["traefik.http.middlewares."+name+"-https.redirectscheme.permanent"] = "true"
	middlewares := []string{}
	if apexes := wwwApexes(routes.domains); len(apexes) > 0 {
		middleware := "traefik.http.middlewares." + name + "-www.redirectregex."
		labels[middleware+"regex"] = escape(`^https?://www\.(` + strings.Join(apexes, "|") + `)(:[0-9]+)?(/.*)?$`)
		labels[middleware+"replacement"] = escape("https://${1}${3}")
		// Permanent: 301, and 308 for a method other than GET.
		labels[middleware+"permanent"] = "true"
		middlewares = append(middlewares, name+"-www")
	}
	if routes.usersFile != "" {
		middlewares = append(middlewares, name+"-auth")
	}
	if len(middlewares) > 0 {
		labels["traefik.http.routers."+name+".middlewares"] = strings.Join(middlewares, ",")
	}
	labels["traefik.http.routers."+name+"-tunnel.middlewares"] = strings.Join(append([]string{name + "-https"}, middlewares...), ",")
}

// wwwApexes are the domains whose www. domain is in the list too, quoted
// for a regular expression.
func wwwApexes(domains []string) []string {
	apexes := []string{}
	for _, domain := range domains {
		if slices.Contains(domains, "www."+domain) {
			apexes = append(apexes, regexp.QuoteMeta(domain))
		}
	}
	return apexes
}

// addInstantLabels labels the instant address, on a router of its own.
func addInstantLabels(labels map[string]any, routes routes) {
	router := routes.router + "-instant"
	labels["traefik.http.routers."+router+".rule"] = "Host(`" + routes.instant + "`)"
	labels["traefik.http.routers."+router+".entrypoints"] = "websecure"
	labels["traefik.http.routers."+router+".service"] = routes.router
	if routes.usersFile != "" {
		labels["traefik.http.routers."+router+".middlewares"] = routes.router + "-auth"
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
	data := filepath.Join(filepath.Dir(a.dir), "traefik")
	// It reads the apps' passwords from the apps folder (releasePassword).
	mounts := []string{
		"-v", "/var/run/docker.sock:/var/run/docker.sock:ro",
		"-v", data + ":/data",
		"-v", a.dir + ":" + proxyApps + ":ro",
	}
	// A proxy on an older image or with other mounts or flags is replaced,
	// so proxy updates reach every server. A label holds a hash of them.
	flags := proxyFlags(mode)
	setup := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(append(slices.Clone(mounts), flags...), "\n"))))[:12]
	running, err := a.output(ctx, "docker", "inspect", "-f", "{{.State.Running}} {{.Config.Image}} {{index .Config.Labels \""+labelMode+"\"}} {{index .Config.Labels \""+labelSetup+"\"}}", proxyName)
	if err == nil && strings.TrimSpace(running) == "true "+proxyImage+" "+mode+" "+setup {
		return nil
	}
	if err == nil {
		_, _ = a.output(ctx, "docker", "rm", "-f", proxyName)
	}
	for _, folder := range []string{data, a.dir} {
		err = os.MkdirAll(folder, 0o700)
		if err != nil {
			return err
		}
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
	args = append(args, mounts...)
	args = append(args, proxyImage)
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
		// Only cloudflared and the apps on this server reach the tunnel
		// entrypoint. Its X-Forwarded-Proto is kept, so the -https
		// middleware sees how the visitor came.
		"--entrypoints." + tunnelEntrypoint + ".forwardedheaders.insecure=true",
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
