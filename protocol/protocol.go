// Package protocol is the contract between the moin agent and the control
// plane. The agent dials out: it registers once over HTTP, then holds one
// WebSocket session. The agent and the control plane both import this package,
// so a change here is a change on both sides.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// Version is the protocol version the agent reports in Hello. Bump it when a
// message changes in a way an older peer cannot read. Version 2 brought
// backups as one history to go back in, without snapshots.
const Version = 2

// Needs is the protocol version an agent must speak for a request: the
// control plane answers "update the agent" to an older one instead of
// sending it.
func Needs(messageType MessageType) int {
	switch messageType {
	case TypeBackupList, TypeBackupRestore, TypeBackupDownload:
		return 2
	}
	return 1
}

const (
	// RegisterPath trades a one-time install token, sent as
	// "Authorization: Bearer <install token>", for an agent token.
	RegisterPath = "/agent/register"
	// SessionPath upgrades to the WebSocket session, authorized with
	// "Authorization: Bearer <agent token>".
	SessionPath = "/agent/session"
)

// MessageType decides how Message.Data decodes.
type MessageType string

const (
	// TypeHello is the first message the agent sends on every session.
	TypeHello MessageType = "hello"

	// TypeDeployStart asks the agent to deploy a release (DeployStart).
	TypeDeployStart MessageType = "deploy.start"
	// TypeDeployLog is one line of deploy output from the agent (DeployLog).
	TypeDeployLog MessageType = "deploy.log"
	// TypeDeployDone ends a deploy (DeployDone).
	TypeDeployDone MessageType = "deploy.done"

	// TypeLogs asks for the last lines of an app's output (LogsRequest).
	// The agent answers with TypeLogsResult or TypeError.
	TypeLogs       MessageType = "logs"
	TypeLogsResult MessageType = "logs.result"

	// TypeAppRemove stops an app and removes its containers and images
	// (AppRemove). Its data directory stays. The agent answers with TypeOK
	// or TypeError.
	TypeAppRemove MessageType = "app.remove"
	// TypeAppStop stops the containers of an app and keeps them, so they
	// stay stopped after a reboot (AppStop). A deploy or a restart starts
	// them again. The agent answers with TypeOK or TypeError.
	TypeAppStop MessageType = "app.stop"

	// TypeDBTables lists the tables of an app's SQLite database
	// (DBRequest). The agent answers with TypeDBTablesResult or TypeError.
	TypeDBTables       MessageType = "db.tables"
	TypeDBTablesResult MessageType = "db.tables.result"
	// TypeDBQuery runs SQL against an app's SQLite database (DBQuery). The
	// agent answers with TypeDBQueryResult or TypeError.
	TypeDBQuery       MessageType = "db.query"
	TypeDBQueryResult MessageType = "db.query.result"

	// TypeBackupConfig tells the agent where backups go (BackupConfig). The
	// control plane sends it after hello and whenever it changes. The agent
	// answers with TypeOK.
	TypeBackupConfig MessageType = "backup.config"
	// TypeBackupList says how the backups of an app stand (BackupRequest).
	// The agent answers with TypeBackupsResult.
	TypeBackupList    MessageType = "backup.list"
	TypeBackupsResult MessageType = "backups.result"
	// TypeBackupRestore puts a database back to a moment, or to the copy
	// before an earlier restore (BackupRestore). The agent answers with
	// TypeOK or TypeError.
	TypeBackupRestore MessageType = "backup.restore"
	// TypeBackupDownload makes a full copy of one database in the bucket's
	// downloads/ folder (BackupRequest). The agent answers with
	// TypeBackupFile or TypeError.
	TypeBackupDownload MessageType = "backup.download"
	TypeBackupFile     MessageType = "backup.file"
	// TypeBackupStatus is how the backups of an app stand (Backups), sent by
	// the agent after a daily check, or when a backup breaks or works again.
	TypeBackupStatus MessageType = "backup.status"

	// TypeServerConfig sets how the server is reached and locked down
	// (ServerConfig). The control plane sends it after hello and whenever
	// it changes. The agent answers with TypeServerConfigResult.
	TypeServerConfig       MessageType = "server.config"
	TypeServerConfigResult MessageType = "server.config.result"

	// TypeAgentUpdate has the agent replace its binary with the one the
	// control plane serves under DownloadPath, check it against SHA256 and
	// restart (AgentUpdate). The agent answers with TypeOK before it
	// restarts, or with TypeError and changes nothing.
	TypeAgentUpdate MessageType = "agent.update"

	// TypeAgentURL tells the agent the control plane's address when it
	// dialed an old one (AgentURL). The agent checks that the address
	// answers, keeps it and dials it from then on. It has no request ID and
	// no answer: an agent that does not know it ignores it.
	TypeAgentURL MessageType = "agent.url"

	// TypeMetrics is a sample of the server and its apps (Metrics), sent by
	// the agent every MetricsEvery without a request. A control plane that
	// does not know it drops it; an agent that does not send it shows no
	// metrics.
	TypeMetrics MessageType = "metrics"

	// TypeExecStart runs one command in a container of an app (ExecStart).
	// The agent answers with TypeExecOutput messages as the command writes,
	// then one TypeExecDone, all with the request's ID. An agent that does
	// not know it answers TypeError.
	TypeExecStart  MessageType = "exec.start"
	TypeExecOutput MessageType = "exec.output"
	TypeExecDone   MessageType = "exec.done"

	// TypeOK answers a request that has no result.
	TypeOK MessageType = "ok"
	// TypeError answers a request that failed (Error).
	TypeError MessageType = "error"
)

// Message is the WebSocket envelope. A request carries a RequestID, and its
// answer carries the same one.
type Message struct {
	Type      MessageType     `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// NewMessage wraps data in an envelope of the given type.
func NewMessage(messageType MessageType, data any) (Message, error) {
	message := Message{Type: messageType}
	if data == nil {
		return message, nil
	}
	body, err := json.Marshal(data)
	if err != nil {
		return Message{}, err
	}
	message.Data = body
	return message, nil
}

// Decode reads Data into target.
func (m Message) Decode(target any) error {
	if len(m.Data) == 0 {
		return errors.New("message has no data")
	}
	return json.Unmarshal(m.Data, target)
}

// Hello describes the agent and its server.
type Hello struct {
	ProtocolVersion int    `json:"protocol_version"`
	AgentVersion    string `json:"agent_version"`
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	// Addresses are the server's own public IPv4 and IPv6 addresses, as far
	// as it knows them. Older agents send none; the control plane then knows
	// only the address the agent connected from.
	Addresses []string `json:"addresses,omitempty"`
	// Rules are the server's own rules for moin, from /etc/moin/rules. Nil
	// means the server has none and moin may do everything.
	Rules *Rules `json:"rules,omitempty"`
}

// Rules are what the owner of a server allows moin to do there, written in
// /etc/moin/rules on the server. The control plane cannot change them; the
// agent refuses every request they do not allow.
type Rules struct {
	// DataOff refuses tables, SQL and row edits.
	DataOff bool `json:"data_off,omitempty"`
	// LogsOff refuses container logs.
	LogsOff bool `json:"logs_off,omitempty"`
	// DeployFrom lists where deploys may come from, such as
	// "github.com/acme/*", "ghcr.io/acme/*" or "upload". Empty allows all.
	DeployFrom []string `json:"deploy_from,omitempty"`
	// BackupsTo is the only bucket backups may go to and come from, as
	// "<endpoint host>/<bucket>". Empty allows any.
	BackupsTo string `json:"backups_to,omitempty"`
	// NetworkLocked refuses changes to the tunnel, Tailscale and firewall.
	NetworkLocked bool `json:"network_locked,omitempty"`
	// ExecOff refuses running commands in containers.
	ExecOff bool `json:"exec_off,omitempty"`
}

// Lines are the rules as written in the rules file, for every surface to
// show them the same way.
func (r *Rules) Lines() []string {
	if r == nil {
		return nil
	}
	var lines []string
	if r.DataOff {
		lines = append(lines, "data: off")
	}
	if r.LogsOff {
		lines = append(lines, "logs: off")
	}
	if len(r.DeployFrom) > 0 {
		lines = append(lines, "deploy-from: "+strings.Join(r.DeployFrom, ", "))
	}
	if r.BackupsTo != "" {
		lines = append(lines, "backups-to: "+r.BackupsTo)
	}
	if r.NetworkLocked {
		lines = append(lines, "network: locked")
	}
	if r.ExecOff {
		lines = append(lines, "exec: off")
	}
	return lines
}

// RegisterRequest is the body of a register call.
type RegisterRequest struct {
	Hello
}

// RegisterResponse answers a register call. The agent stores the token and
// uses it for every session.
type RegisterResponse struct {
	ServerID   string `json:"server_id"`
	AgentToken string `json:"agent_token"`
}

// DeployStart is one deploy. Number counts per app. A rollback carries
// FromNumber and no source: the agent starts that release's images again,
// with the env it had. Image replaces the build of the main service with an
// image built elsewhere, such as in CI; without a source the agent takes the
// compose file of the running release. Without source, image and FromNumber
// the deploy applies env and routing to the running release, without a
// build.
type DeployStart struct {
	DeployID string `json:"deploy_id"`
	App      string `json:"app"`
	// AppID is the app's ID in the control plane. Its backups live under
	// it, so an app removed and created again with the same name starts
	// with an empty backup folder.
	AppID     string `json:"app_id,omitempty"`
	Number    int    `json:"number"`
	SourceURL string `json:"source_url,omitempty"`
	// SourceAuthorization is sent with the download, such as a GitHub
	// token; SourceStripTop drops the one folder GitHub wraps a repo in.
	// SourceDeleteURL deletes the source from the user's bucket once it is
	// downloaded. The control plane never holds the source itself.
	SourceAuthorization string `json:"source_authorization,omitempty"`
	SourceStripTop      bool   `json:"source_strip_top,omitempty"`
	SourceDeleteURL     string `json:"source_delete_url,omitempty"`
	// SourceNumber builds the source the server kept of release n again,
	// for a redeploy; nothing is downloaded.
	SourceNumber int               `json:"source_number,omitempty"`
	SHA256       string            `json:"sha256,omitempty"`
	FromNumber   int               `json:"from_number,omitempty"`
	Image        string            `json:"image,omitempty"`
	Registry     *Registry         `json:"registry,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Routing      *Routing          `json:"routing,omitempty"`
}

// Routing is how visitors reach the main service, as set in web, CLI, TUI
// or MCP. The x-moin block of compose.yml wins for every field it sets.
// Protect is an htpasswd line ("user:bcrypt hash") when password protection
// is on.
type Routing struct {
	Domains   []string `json:"domains,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
	Protect   string   `json:"protect,omitempty"`
	// Services are the domains of the app's other services, by service.
	Services map[string][]string `json:"services,omitempty"`
	// Instant is the provider of the app's instant address, such as
	// sslip.io, "" for none; InstantIP the server's public IPv4 it names.
	// InstantProtect is the htpasswd line in front of it, "" when open.
	Instant        string `json:"instant,omitempty"`
	InstantIP      string `json:"instant_ip,omitempty"`
	InstantProtect string `json:"instant_protect,omitempty"`
}

// InstantProviders are the services that name an IP without any setup:
// <name>.<ip with dashes>.<provider> resolves to the IP.
var InstantProviders = []string{"sslip.io", "nip.io"}

// InstantHost is the instant address of an app on a server with the public
// IPv4 ip, such as shop.203-0-113-7.sslip.io, or "" when the provider is
// none of InstantProviders or the IP is not IPv4.
func InstantHost(app, ip, provider string) string {
	address, err := netip.ParseAddr(ip)
	if err != nil || !address.Is4() || !slices.Contains(InstantProviders, provider) || app == "" {
		return ""
	}
	return app + "." + strings.ReplaceAll(address.String(), ".", "-") + "." + provider
}

// Registry is a login for pulling Image from a private registry.
type Registry struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// DeployLog is one line of build or start output.
type DeployLog struct {
	DeployID string `json:"deploy_id"`
	Line     string `json:"line"`
}

// DeployDone reports how a deploy ended and what the agent found.
type DeployDone struct {
	DeployID string   `json:"deploy_id"`
	OK       bool     `json:"ok"`
	Error    string   `json:"error,omitempty"`
	Detected Detected `json:"detected"`
	// Before is the moment, by the server's clock, just before the new
	// release started over an older one: the control plane remembers it,
	// so a restore can go back to it.
	Before *time.Time `json:"before,omitempty"`
}

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
type Database struct {
	Path     string `json:"path"`
	HostPath string `json:"host_path"`
	Kind     string `json:"kind,omitempty"`
	Service  string `json:"service,omitempty"`
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

// ExecStart runs Argv in a container of Service of App, as a list: no shell
// reads it. The agent stops it after Timeout seconds.
type ExecStart struct {
	App     string   `json:"app"`
	Service string   `json:"service"`
	Argv    []string `json:"argv"`
	Timeout int      `json:"timeout"`
}

// ExecOutput is a piece of what the command wrote, stdout and stderr as
// they came.
type ExecOutput struct {
	Data string `json:"data"`
}

// ExecDone ends a run: the exit code, or the error that kept the command
// from running. Truncated says the output passed ExecMaxOutput and the rest
// was dropped; TimedOut that the agent stopped it.
type ExecDone struct {
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	Error     string `json:"error,omitempty"`
}

const (
	// ExecMaxOutput is how much a run may write before the rest is dropped.
	ExecMaxOutput = 1 << 20
	// ExecTimeout is how long a run may take unless asked otherwise, and
	// ExecMaxTimeout how long it may take at most, in seconds.
	ExecTimeout    = 10 * 60
	ExecMaxTimeout = 60 * 60
)

// LogsRequest asks for the last Tail lines of every container of an app.
type LogsRequest struct {
	App  string `json:"app"`
	Tail int    `json:"tail"`
}

// LogsResult carries the lines, oldest first.
type LogsResult struct {
	Lines []string `json:"lines"`
}

// AppRemove names the app to remove.
type AppRemove struct {
	App string `json:"app"`
}

// AppStop names the app to stop.
type AppStop struct {
	App string `json:"app"`
}

// DBRequest names an app's database by its path in the container, as in
// Detected.Databases. An empty Database means the first one.
type DBRequest struct {
	App      string `json:"app"`
	Database string `json:"database,omitempty"`
}

// DBTables lists the tables and views of a database, and the newest
// migration a known migration tool recorded in it.
type DBTables struct {
	Database  string       `json:"database"`
	Tables    []DBTable    `json:"tables"`
	Migration *DBMigration `json:"migration,omitempty"`
}

// DBMigration is the newest migration of goose, golang-migrate, Prisma or
// Drizzle. The app runs its migrations; moin only reads where they are.
type DBMigration struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	AppliedAt string `json:"applied_at,omitempty"`
}

// DBTable is one table or view. RowID is false for views and WITHOUT ROWID
// tables: their rows cannot be edited by rowid.
type DBTable struct {
	Name    string     `json:"name"`
	Type    string     `json:"type"`
	Rows    int64      `json:"rows"`
	RowID   bool       `json:"rowid"`
	Columns []DBColumn `json:"columns"`
	// SQL is the CREATE statement, Indexes the CREATE INDEX statements.
	SQL     string   `json:"sql,omitempty"`
	Indexes []string `json:"indexes,omitempty"`
}

// DBColumn is one column of a table.
type DBColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null,omitempty"`
	PrimaryKey bool   `json:"primary_key,omitempty"`
}

// DBQuery runs SQL with positional parameters. A read opens the database
// read-only and returns at most Limit rows. A write runs in one
// transaction and returns the number of changed rows.
type DBQuery struct {
	DBRequest
	SQL    string `json:"sql"`
	Params []any  `json:"params,omitempty"`
	Write  bool   `json:"write,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// DBResult is the answer to a DBQuery. Values are null, numbers or text;
// a blob that is not text comes as "0x" and its hex. Truncated is true when
// the query had more rows than the limit.
type DBResult struct {
	Database  string   `json:"database"`
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated,omitempty"`
	Changes   int64    `json:"changes,omitempty"`
	// Before is the moment, by the server's clock, just before a write:
	// the control plane remembers it, so a restore can go back to it.
	Before *time.Time `json:"before,omitempty"`
}

// BackupStorage is an S3-compatible bucket. Everything of a workspace
// goes under <Path>/<Workspace>/, each app under <app>/<app ID>/:
//
//	<app>/<app ID>/uploads/                      sources of moin deploy, a day
//	<app>/<app ID>/downloads/                    copies to download, a day
//	<app>/<app ID>/<database>/litestream/        every change of a SQLite file, 7 days
//	<app>/<app ID>/<database>/walg/              every change of a Postgres, 7 days
//	<app>/<app ID>/<database>/before-restore/    the copy before each restore, 7 days
//
// Workspace is the workspace's ID, which never changes, so workspaces
// sharing one bucket never touch each other's files. Name is the bucket's
// name in moin.
type BackupStorage struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Path      string `json:"path,omitempty"`
	Workspace string `json:"workspace"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Name      string `json:"name,omitempty"`
}

// BackupConfig is where backups go. Storage is the workspace's default
// bucket; without it the agent stops recording. AppStorage is the bucket
// of each app, by name, that uses another one than the default. Apps are
// the IDs of the server's apps by name: an app deployed before deploys
// brought its ID gets it here, so its backups start without another
// deploy.
type BackupConfig struct {
	Storage    *BackupStorage            `json:"storage,omitempty"`
	AppStorage map[string]*BackupStorage `json:"app_storage,omitempty"`
	Apps       map[string]string         `json:"apps,omitempty"`
}

// BackupRequest names an app; Database narrows it to one database.
type BackupRequest struct {
	App      string `json:"app"`
	Database string `json:"database,omitempty"`
}

// Backups are the backups of an app's databases.
type Backups struct {
	App       string           `json:"app"`
	Databases []DatabaseBackup `json:"databases"`
}

// DatabaseBackup is how one database is backed up. Since is the oldest
// moment a restore can go back to, ReplicatedAt the newest change in the
// bucket. VerifiedAt is the newest daily check that passed: it restored
// the latest state into a throwaway copy and checked it. Copies are the
// copies moin took before each restore of the last 7 days, newest first:
// going back to one undoes that restore.
type DatabaseBackup struct {
	Database     string     `json:"database"`
	Kind         string     `json:"kind,omitempty"`
	Since        *time.Time `json:"since,omitempty"`
	ReplicatedAt *time.Time `json:"replicated_at,omitempty"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
	Copies       []Copy     `json:"copies,omitempty"`
	Error        string     `json:"error,omitempty"`
	// DrillError is why the newest daily check failed. Error holds it too
	// when nothing else is wrong.
	DrillError string `json:"drill_error,omitempty"`
	// Note says what a database lacks, such as backups for a Postgres
	// image WAL-G does not run in.
	Note string `json:"note,omitempty"`
}

// Copy is the full copy of a database moin took before a restore. Its ID
// is the time it was taken, in CopyIDLayout.
type Copy struct {
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
	Size int64     `json:"size"`
}

// BackupRestore puts one database back: to the moment At, or to the copy
// Copy took before an earlier restore, which undoes that restore. Either
// way the agent first takes one full copy of the database as it is, so
// this restore can be undone too. The app stops for the swap and starts
// again.
type BackupRestore struct {
	App      string     `json:"app"`
	Database string     `json:"database,omitempty"`
	At       *time.Time `json:"at,omitempty"`
	Copy     string     `json:"copy,omitempty"`
}

// BackupFile is a full copy of a database the agent put in the bucket for
// a download: a gzipped SQLite file, or a pg_dump in its custom format.
// Key is where it is in the app's bucket; it goes after a day.
type BackupFile struct {
	Database string `json:"database"`
	Key      string `json:"key"`
	Size     int64  `json:"size"`
}

// ServerConfig is how a server is reached and locked down.
//
// TunnelToken runs cloudflared, which carries all app traffic from
// Cloudflare to Traefik; the proxy then opens no port. Without it Traefik
// takes 80 and 443. TailscaleKey joins the server to the user's tailnet;
// SSHOnTailscale then answers SSH only there. Firewall drops every other
// inbound connection.
type ServerConfig struct {
	TunnelToken    string `json:"tunnel_token,omitempty"`
	TailscaleKey   string `json:"tailscale_key,omitempty"`
	SSHOnTailscale bool   `json:"ssh_on_tailscale,omitempty"`
	Firewall       bool   `json:"firewall,omitempty"`
}

// ServerConfigResult is what the agent set up. Problems are the parts it
// could not, in words; the rest stands.
type ServerConfigResult struct {
	Proxy       string   `json:"proxy"`
	TailscaleIP string   `json:"tailscale_ip,omitempty"`
	Firewall    bool     `json:"firewall"`
	OpenPorts   []string `json:"open_ports"`
	Problems    []string `json:"problems,omitempty"`
}

// MetricsEvery is how often the agent samples its server and apps and sends
// the sample.
const MetricsEvery = 15 * time.Second

// Metrics is one sample of a server and the apps on it. Rates are per
// second since the sample before. Server is nil where the agent cannot read
// the server's numbers, such as outside Linux.
type Metrics struct {
	At     time.Time      `json:"at"`
	Server *ServerMetrics `json:"server,omitempty"`
	Apps   []AppMetrics   `json:"apps,omitempty"`
}

// ServerMetrics are the numbers of the whole server. Disk is the disk that
// holds the apps' data.
type ServerMetrics struct {
	// CPU is the busy share of all cores in percent, 0 to 100.
	CPU         float64 `json:"cpu"`
	Cores       int     `json:"cores,omitempty"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryTotal uint64  `json:"memory_total"`
	DiskUsed    uint64  `json:"disk_used"`
	DiskTotal   uint64  `json:"disk_total"`
	// NetRx and NetTx are bytes per second in and out over the server's
	// own interfaces, not Docker's bridges.
	NetRx float64 `json:"net_rx"`
	NetTx float64 `json:"net_tx"`
	// Load is the load average over one minute.
	Load float64 `json:"load"`
}

// AppMetrics are the numbers of the running containers of one app, added
// up.
type AppMetrics struct {
	App string `json:"app"`
	// CPU is the app's share of the whole server in percent, so the apps
	// add up to at most the server's CPU.
	CPU        float64 `json:"cpu"`
	MemoryUsed uint64  `json:"memory_used"`
	// MemoryLimit is the sum of the containers' limits; 0 means no limit,
	// so the server's memory is the limit.
	MemoryLimit uint64  `json:"memory_limit,omitempty"`
	NetRx       float64 `json:"net_rx"`
	NetTx       float64 `json:"net_tx"`
	Containers  int     `json:"containers"`
}

// Error is the answer to a request that failed.
type Error struct {
	Message string `json:"message"`
}

// DownloadPath is where the control plane serves the binaries, as
// <name>-<os>-<arch>: the install script and agent updates take them from
// there.
const DownloadPath = "/download/"

// AgentURL is the address of the control plane an agent should dial.
type AgentURL struct {
	URL string `json:"url"`
}

// AgentUpdate is the binary the agent should run: the control plane's
// version and the SHA-256 of the binary it serves for the agent's OS and
// CPU. The agent downloads it from its own control plane URL.
type AgentUpdate struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// UpdateCommand updates an agent by hand on the server, as root: for an
// agent too old to update itself, or one whose rules keep it from doing so.
func UpdateCommand(baseURL, name, arch string) string {
	binary := "/usr/local/bin/" + name
	return fmt.Sprintf("curl -fsSL %s%s%s-linux-%s -o %s.new && chmod 755 %s.new && mv %s.new %s && systemctl restart %s-agent",
		strings.TrimRight(baseURL, "/"), DownloadPath, name, arch, binary, binary, binary, binary, name)
}

// AgentUnit is the systemd unit of the agent.
func AgentUnit(name string) string {
	return "[Unit]\n" +
		"Description=" + name + " agent\n" +
		"After=network-online.target docker.service\n" +
		"Wants=network-online.target docker.service\n" +
		"\n[Service]\n" +
		"ExecStart=/usr/local/bin/" + name + " agent\n" +
		"Environment=" + strings.ToUpper(name) + "_STATE_DIR=/var/lib/" + name + "\n" +
		"Restart=always\n" +
		"RestartSec=2\n" +
		"\n[Install]\n" +
		"WantedBy=multi-user.target\n"
}

// UninstallCommand removes the agent from a server, run on it as root: its
// service, binary and state. The apps keep running with their data; add
// --apps to remove them too.
func UninstallCommand(name string) string {
	return name + " agent uninstall"
}
