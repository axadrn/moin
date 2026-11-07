package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Detected is what the agent learned about an app from its repo and its
// running containers. Config holds the x-moin block of compose.yml, if any.
type Detected struct {
	// Kind is "compose", "dockerfile", "go" or "railpack": what the repo
	// brought, and for railpack that moin built it with Railpack. Builder
	// says what Railpack found, like "Node 24.22.1, start: npm start".
	// Image is the image of the main service when it came from CI.
	// Domains are the domains the main service answers on, Canonical the one
	// the others redirect to, Protected whether a password guards it.
	Kind      string   `json:"kind"`
	Builder   string   `json:"builder,omitempty"`
	Image     string   `json:"image,omitempty"`
	Domains   []string `json:"domains,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
	Protected bool     `json:"protected,omitempty"`
	// ServiceDomains are the domains of the other services, by service;
	// Instant is the main service's instant address, "" when it has none.
	ServiceDomains map[string][]string `json:"service_domains,omitempty"`
	Instant        string              `json:"instant,omitempty"`
	Service        string              `json:"service"`
	Port           int                 `json:"port"`
	Databases      []Database          `json:"databases"`
	Config         *Config             `json:"config,omitempty"`
}

// Built says for people how moin built the app: the language Railpack found,
// like "Node 24.22.1 (Railpack)", or the kind.
func (d Detected) Built() string {
	if d.Kind == "railpack" && d.Builder != "" {
		language, _, _ := strings.Cut(d.Builder, ", start:")
		return language + " (Railpack)"
	}
	return d.Kind
}

// Database is a database of an app: a SQLite file the agent found on one
// of the app's mounts, or, with Kind "postgres", a compose service that runs
// Postgres. Path names a SQLite file in the container; for Postgres it is
// postgres-<service>, and HostPath is empty. Older agents send no Kind.
// Name is a database of a Postgres service besides the one POSTGRES_DB
// names, which backups copy too; its Path is postgres-<service>/<name>.
type Database struct {
	Path     string `json:"path"`
	HostPath string `json:"host_path"`
	Kind     string `json:"kind,omitempty"`
	Service  string `json:"service,omitempty"`
	Name     string `json:"name,omitempty"`
}

// DatabasePostgres is the Kind of a Postgres service.
const DatabasePostgres = "postgres"

// Postgres reports whether the database is a Postgres service.
func (d Database) Postgres() bool { return d.Kind == DatabasePostgres }

// PostgresKey is the name of a Postgres service's database in moin: in the
// bucket, the API and on every surface.
func PostgresKey(service string) string { return "postgres-" + service }

// OldConfigKey is the block of compose.yml under the name the product had
// before. The agent refuses it with a message to rename it to x-moin.
const OldConfigKey = "x-ops"

// Config is the x-moin block of compose.yml. Every field is optional.
type Config struct {
	App       string   `json:"app,omitempty"`
	Server    string   `json:"server,omitempty"`
	Service   string   `json:"service,omitempty"`
	Port      int      `json:"port,omitempty"`
	Health    string   `json:"health,omitempty"`
	Domains   []string `json:"domains,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
	Protect   bool     `json:"protect,omitempty"`
	Database  string   `json:"database,omitempty"`
	// ServiceDomains are domains by service, from the map form of domains:
	// domains: {web: [shop.com], api: [api.shop.com]}. Ports are the ports
	// of services moin cannot tell. Instant picks the instant address
	// provider, or off.
	ServiceDomains map[string][]string `json:"service_domains,omitempty"`
	Ports          map[string]int      `json:"ports,omitempty"`
	Instant        string              `json:"instant,omitempty"`
}

// UnmarshalJSON reads domains as a list, for the main service, or as a map
// of service to domains.
func (c *Config) UnmarshalJSON(body []byte) error {
	type plain Config
	var raw struct {
		plain
		Domains json.RawMessage `json:"domains,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	*c = Config(raw.plain)
	c.Domains = nil
	domains := bytes.TrimSpace(raw.Domains)
	switch {
	case len(domains) == 0 || string(domains) == "null":
	case domains[0] == '{':
		byService := map[string][]string{}
		if err := json.Unmarshal(domains, &byService); err != nil {
			return fmt.Errorf("x-moin domains: %w", err)
		}
		if c.ServiceDomains == nil {
			c.ServiceDomains = map[string][]string{}
		}
		for service, list := range byService {
			c.ServiceDomains[service] = list
		}
	default:
		if err := json.Unmarshal(domains, &c.Domains); err != nil {
			return fmt.Errorf("x-moin domains: %w", err)
		}
	}
	return nil
}
