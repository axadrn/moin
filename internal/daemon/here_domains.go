package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/axadrn/moin/protocol"
)

// An app's domains live on the server in apps/<app>/domains, one per line.
// A line is a domain, or a domain and the service it reaches, like
// "api.example.com api". Without a service it reaches the main one. Every
// deploy reads them; the instant address comes on top.

// Domain is one line of the file: a domain, and the service it reaches,
// "" for the main one.
type Domain struct {
	Domain  string `json:"domain"`
	Service string `json:"service,omitempty"`
}

// domainsFile is the file of the app's domains.
func (a *apps) domainsFile(app string) string { return filepath.Join(a.appDir(app), "domains") }

// domainList are the lines of the app's file, none when it has no file.
func (a *apps) domainList(app string) []Domain {
	body, _ := os.ReadFile(a.domainsFile(app))
	list := []Domain{}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		domain := Domain{Domain: fields[0]}
		if len(fields) > 1 {
			domain.Service = fields[1]
		}
		list = append(list, domain)
	}
	return list
}

// routing is what a deploy learns from the file: the main service's
// domains, and the other services' by service.
func (a *apps) routing(app string) *protocol.Domains {
	routing := &protocol.Domains{Instant: "sslip.io", InstantIP: firstIPv4()}
	for _, line := range a.domainList(app) {
		if line.Service == "" {
			routing.Domains = append(routing.Domains, line.Domain)
			continue
		}
		if routing.Services == nil {
			routing.Services = map[string][]string{}
		}
		routing.Services[line.Service] = append(routing.Services[line.Service], line.Domain)
	}
	return routing
}

// DomainsHere lists the app's domains.
func DomainsHere(dir, app string) ([]Domain, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return nil, err
	}
	return a.domainList(app), nil
}

// AddDomainHere gives the app one more domain and starts it with it, without
// a build. The domain reaches service, "" for the main one. When that
// fails, the domains stay as they were.
func AddDomainHere(ctx context.Context, dir, app, domain, service string, out io.Writer) (Result, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Result{}, err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !domainPattern.MatchString(domain) {
		return Result{}, fmt.Errorf("%q is not a domain name, like my-app.example.com", domain)
	}
	if owner := a.domainOwner(domain); owner != "" {
		return Result{}, fmt.Errorf("%s belongs to %s already", domain, owner)
	}
	if service != "" {
		detected, _ := a.live(app)
		services := a.services(app)
		if !slices.Contains(services, service) {
			return Result{}, fmt.Errorf("%s has no service %s, only %s", app, service, strings.Join(services, ", "))
		}
		if service == detected.Service {
			service = ""
		}
	}
	return a.changeDomains(ctx, app, append(a.domainList(app), Domain{Domain: domain, Service: service}), out)
}

// RemoveDomainHere takes a domain off the app and starts it without it.
func RemoveDomainHere(ctx context.Context, dir, app, domain string, out io.Writer) (Result, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Result{}, err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	list := a.domainList(app)
	rest := slices.DeleteFunc(slices.Clone(list), func(line Domain) bool { return line.Domain == domain })
	if len(rest) == len(list) {
		return Result{}, fmt.Errorf("%s has no domain %s", app, domain)
	}
	return a.changeDomains(ctx, app, rest, out)
}

// changeDomains writes the new list and applies it. A failed deploy puts
// the old list back, as the old release runs again.
func (a *apps) changeDomains(ctx context.Context, app string, list []Domain, out io.Writer) (Result, error) {
	old, _ := os.ReadFile(a.domainsFile(app))
	lines := ""
	for _, line := range list {
		lines += strings.TrimSpace(line.Domain+" "+line.Service) + "\n"
	}
	if err := writeWhole(a.domainsFile(app), []byte(lines)); err != nil {
		return Result{}, err
	}
	result, err := a.deployHere(ctx, a.newStart(app), out)
	if err != nil {
		_ = writeWhole(a.domainsFile(app), old)
	}
	return result, err
}

// domainOwner is the app on this server that has domain, "" for none.
func (a *apps) domainOwner(domain string) string {
	for _, app := range a.names() {
		for _, line := range a.domainList(app) {
			if line.Domain == domain {
				return app
			}
		}
	}
	return ""
}

// services are the names of the services in the app's live compose file,
// sorted.
func (a *apps) services(app string) []string {
	body, _ := os.ReadFile(a.composeJSON(app))
	var config struct {
		Services map[string]json.RawMessage `json:"services"`
	}
	_ = json.Unmarshal(body, &config)
	return slices.Sorted(maps.Keys(config.Services))
}
