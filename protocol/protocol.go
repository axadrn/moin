// Package protocol is the contract between the ops agent and the control
// plane. The agent dials out: it registers once over HTTP, then holds one
// WebSocket session. Both repos import this package, so a change here is a
// change on both sides.
package protocol

import (
	"encoding/json"
	"errors"
)

// Version is the protocol version the agent reports in Hello. Bump it when a
// message changes in a way an older peer cannot read.
const Version = 1

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

	// TypeDBTables lists the tables of an app's SQLite database
	// (DBRequest). The agent answers with TypeDBTablesResult or TypeError.
	TypeDBTables       MessageType = "db.tables"
	TypeDBTablesResult MessageType = "db.tables.result"
	// TypeDBQuery runs SQL against an app's SQLite database (DBQuery). The
	// agent answers with TypeDBQueryResult or TypeError.
	TypeDBQuery       MessageType = "db.query"
	TypeDBQueryResult MessageType = "db.query.result"

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
	DeployID   string            `json:"deploy_id"`
	App        string            `json:"app"`
	Number     int               `json:"number"`
	SourceURL  string            `json:"source_url,omitempty"`
	SHA256     string            `json:"sha256,omitempty"`
	FromNumber int               `json:"from_number,omitempty"`
	Image      string            `json:"image,omitempty"`
	Registry   *Registry         `json:"registry,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Routing    *Routing          `json:"routing,omitempty"`
}

// Routing is how visitors reach the main service, as set in web, CLI, TUI
// or MCP. The x-ops block of compose.yml wins for every field it sets.
// Protect is an htpasswd line ("user:bcrypt hash") when password protection
// is on.
type Routing struct {
	Domains   []string `json:"domains,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
	Protect   string   `json:"protect,omitempty"`
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
}

// Detected is what the agent learned about an app from its repo and its
// running containers. Config holds the x-ops block of compose.yml, if any.
type Detected struct {
	// Kind is "compose", "dockerfile" or "go": what the repo brought.
	// Image is the image of the main service when it came from CI.
	// Domains are the domains the main service answers on, Canonical the one
	// the others redirect to, Protected whether a password guards it.
	Kind      string     `json:"kind"`
	Image     string     `json:"image,omitempty"`
	Domains   []string   `json:"domains,omitempty"`
	Canonical string     `json:"canonical,omitempty"`
	Protected bool       `json:"protected,omitempty"`
	Service   string     `json:"service"`
	Port      int        `json:"port"`
	Databases []Database `json:"databases"`
	Config    *Config    `json:"config,omitempty"`
}

// Database is a SQLite file the agent found on one of the app's mounts.
type Database struct {
	Path     string `json:"path"`
	HostPath string `json:"host_path"`
}

// Config is the x-ops block of compose.yml. Every field is optional.
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
}

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
// Drizzle. The app runs its migrations; ops only reads where they are.
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
}

// Error is the answer to a request that failed.
type Error struct {
	Message string `json:"message"`
}
