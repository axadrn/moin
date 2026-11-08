// Package protocol is the contract between the moin daemon and the hub.
// The daemon dials out: it registers once over HTTP, then holds one
// WebSocket session. The daemon and the hub both import this package,
// so a change here is a change on both sides.
package protocol

import (
	"encoding/json"
	"errors"
)

// Version is the protocol version the daemon reports in Hello. Bump it when a
// message changes in a way an older peer cannot read. Version 2 brought
// backups as copies on a schedule, restores and undos; version 3 the daemon,
// which was called the agent before (legacy.go); version 4 SQLite backed up
// by Litestream alone: no copies, always every change, and the download.
const Version = 4

// Needs is the protocol version a daemon must speak for a request: the
// hub answers "update the daemon" to an older one instead of
// sending it.
func Needs(messageType MessageType) int {
	switch messageType {
	case TypeBackupList, TypeBackupRestore, TypeBackupRestores:
		return 2
	case TypeBackupConfig, TypeBackupDownload:
		// An older daemon reads a schedule without every_change as off and
		// deletes what Litestream wrote.
		return 4
	}
	return 1
}

const (
	// RegisterPath trades a one-time install token, sent as
	// "Authorization: Bearer <install token>", for a daemon token.
	RegisterPath = "/daemon/register"
	// SessionPath upgrades to the WebSocket session, authorized with
	// "Authorization: Bearer <daemon token>".
	SessionPath = "/daemon/session"
)

// MessageType decides how Message.Data decodes.
type MessageType string

const (
	// TypeHello is the first message the daemon sends on every session.
	TypeHello MessageType = "hello"

	// TypeDeployStart asks the daemon to deploy an app (DeployStart).
	TypeDeployStart MessageType = "deploy.start"
	// TypeDeployLog is one line of deploy output from the daemon (DeployLog).
	TypeDeployLog MessageType = "deploy.log"
	// TypeDeployDone ends a deploy (DeployDone).
	TypeDeployDone MessageType = "deploy.done"

	// TypeLogs asks for the last lines of an app's output (LogsRequest).
	// The daemon answers with TypeLogsResult or TypeError.
	TypeLogs       MessageType = "logs"
	TypeLogsResult MessageType = "logs.result"

	// TypeAppRemove stops an app and removes its containers and images
	// (AppRemove). Its data directory stays. The daemon answers with TypeOK
	// or TypeError.
	TypeAppRemove MessageType = "app.remove"
	// TypeAppStop stops the containers of an app and keeps them, so they
	// stay stopped after a reboot (AppStop). A deploy or a restart starts
	// them again. The daemon answers with TypeOK or TypeError.
	TypeAppStop MessageType = "app.stop"

	// TypeDBTables lists the tables of an app's database, a SQLite file or
	// a Postgres service (DBRequest). The daemon answers with
	// TypeDBTablesResult or TypeError.
	TypeDBTables       MessageType = "db.tables"
	TypeDBTablesResult MessageType = "db.tables.result"
	// TypeDBQuery runs SQL against an app's database, SQLite or Postgres
	// (DBQuery). The daemon answers with TypeDBQueryResult or TypeError.
	TypeDBQuery       MessageType = "db.query"
	TypeDBQueryResult MessageType = "db.query.result"

	// TypeBackupConfig tells the daemon where backups go and how often
	// (BackupConfig). The
	// hub sends it after hello and whenever it changes. The daemon
	// answers with TypeOK.
	TypeBackupConfig MessageType = "backup.config"
	// TypeBackupList says how the backups of an app stand (BackupRequest).
	// The daemon answers with TypeBackupsResult.
	TypeBackupList    MessageType = "backup.list"
	TypeBackupsResult MessageType = "backups.result"
	// TypeBackupRestore puts a database back to a copy, a minute or the
	// state before an earlier restore (BackupRestore). The daemon answers
	// with TypeOK or TypeError.
	TypeBackupRestore MessageType = "backup.restore"
	// TypeBackupRestores asks how the restores of an app stand
	// (BackupRequest), without looking into the bucket: also when the app
	// has no bucket or is gone. The daemon answers with TypeBackupsResult,
	// whose Restores is the answer and Databases empty.
	TypeBackupRestores MessageType = "backup.restores"
	// TypeBackupDownload has the daemon make a download of the newest state
	// of a SQLite database in the bucket (BackupRequest). It answers with
	// TypeBackupDownloadResult, a Copy with the download's ID.
	TypeBackupDownload       MessageType = "backup.download"
	TypeBackupDownloadResult MessageType = "backup.download.result"
	// TypeBackupStatus is how the backups of an app stand (Backups), sent by
	// the daemon after a daily check or a restore, or when a backup breaks
	// or works again.
	TypeBackupStatus MessageType = "backup.status"

	// TypeServerConfig sets how the server is reached and locked down
	// (ServerConfig). The hub sends it after hello and whenever
	// it changes. The daemon answers with TypeServerConfigResult.
	TypeServerConfig       MessageType = "server.config"
	TypeServerConfigResult MessageType = "server.config.result"

	// TypeDaemonUpdate has the daemon replace its binary with the one the
	// hub serves under DownloadPath, check it against SHA256 and
	// restart (DaemonUpdate). The daemon answers with TypeOK before it
	// restarts, or with TypeError and changes nothing.
	TypeDaemonUpdate MessageType = "daemon.update"

	// TypeDaemonURL tells the daemon the hub's address when it
	// dialed an old one (DaemonURL). The daemon checks that the address
	// answers, keeps it and dials it from then on. It has no request ID and
	// no answer: a daemon that does not know it ignores it.
	TypeDaemonURL MessageType = "daemon.url"

	// TypeMetrics is a sample of the server and its apps (Metrics), sent by
	// the daemon every MetricsEvery without a request. A hub that
	// does not know it drops it; a daemon that does not send it shows no
	// metrics.
	TypeMetrics MessageType = "metrics"

	// TypeExecStart runs one command in a container of an app (ExecStart).
	// The daemon answers with TypeExecOutput messages as the command writes,
	// then one TypeExecDone, all with the request's ID. A daemon that does
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

// Error is the answer to a request that failed.
type Error struct {
	Message string `json:"message"`
}
