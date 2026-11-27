package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// An app's domains live on the server in apps/<app>/domains, one per line.
// Every deploy reads them; the instant address comes on top.

// domainsFile is the file of the app's domains.
func (a *apps) domainsFile(app string) string { return filepath.Join(a.appDir(app), "domains") }

// domains are the app's domains, none when it has no file.
func (a *apps) domains(app string) []string {
	body, _ := os.ReadFile(a.domainsFile(app))
	return strings.Fields(string(body))
}

// DomainsHere lists the app's domains.
func DomainsHere(dir, app string) ([]string, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return nil, err
	}
	return a.domains(app), nil
}

// AddDomainHere gives the app one more domain and starts it with it, without
// a build. When that fails, the domains stay as they were.
func AddDomainHere(ctx context.Context, dir, app, domain string, out io.Writer) (Result, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Result{}, err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !domainPattern.MatchString(domain) {
		return Result{}, fmt.Errorf("%q is not a domain name, like shop.example.com", domain)
	}
	if owner := a.domainOwner(domain); owner != "" {
		return Result{}, fmt.Errorf("%s belongs to %s already", domain, owner)
	}
	return a.changeDomains(ctx, app, append(a.domains(app), domain), out)
}

// RemoveDomainHere takes a domain off the app and starts it without it.
func RemoveDomainHere(ctx context.Context, dir, app, domain string, out io.Writer) (Result, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Result{}, err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	list := a.domains(app)
	if !slices.Contains(list, domain) {
		return Result{}, fmt.Errorf("%s has no domain %s", app, domain)
	}
	return a.changeDomains(ctx, app, slices.DeleteFunc(list, func(d string) bool { return d == domain }), out)
}

// changeDomains writes the new list and applies it. A failed deploy puts
// the old list back, as the old release runs again.
func (a *apps) changeDomains(ctx context.Context, app string, list []string, out io.Writer) (Result, error) {
	old, _ := os.ReadFile(a.domainsFile(app))
	if err := writeWhole(a.domainsFile(app), []byte(strings.Join(list, "\n")+"\n")); err != nil {
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
		if slices.Contains(a.domains(app), domain) {
			return app
		}
	}
	return ""
}
