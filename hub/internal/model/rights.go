package model

import (
	"database/sql/driver"
	"slices"
	"strings"
)

// Rights are what a connection or a CI token may do, a few switches the
// person sets when they let a client in. Read is always on. The rights
// never add to a role: a member's connection still cannot invite, however
// many rights it has.
type Rights uint8

const (
	// RightRead looks: every GET, logs, metrics, status, the streams.
	RightRead Rights = 1 << iota
	// RightDeploy runs and changes apps: deploy, restart, stop, an app's
	// settings.
	RightDeploy
	// RightEnv is an app's secrets and data: env vars and its password,
	// database rows, SQL that reads and copies of the databases. The name
	// stays env; the switch says "Secrets and data".
	RightEnv
	// RightDelete removes what is hard to get back: apps, buckets, data.
	RightDelete
	// RightServers manages what is around the apps: servers, the
	// workspace, the team, your account.
	RightServers
)

// DefaultRights are the switches a new connection or CI token starts with,
// the same for every client.
const DefaultRights = RightRead | RightDeploy | RightEnv

// AllRights are every right there is, in the order they are shown.
var AllRights = []Rights{RightRead, RightDeploy, RightEnv, RightDelete, RightServers}

// rightNames are the names of the rights in the database, the API and the
// docs.
var rightNames = map[Rights]string{
	RightRead:    "read",
	RightDeploy:  "deploy",
	RightEnv:     "env",
	RightDelete:  "delete",
	RightServers: "servers",
}

// Name is the name of one right: "deploy".
func (r Rights) Name() string { return rightNames[r] }

// Label is what a switch of the right says, on the approval page and in
// Settings.
func (r Rights) Label() string {
	switch r {
	case RightRead:
		return "Read: apps, logs, status"
	case RightDeploy:
		return "Deploy and restart"
	case RightEnv:
		return "Secrets and data"
	case RightDelete:
		return "Delete apps, buckets, data"
	case RightServers:
		return "Manage servers and settings"
	}
	return ""
}

// Verb is what the right lets a client do, for "This connection may not
// <verb>."
func (r Rights) Verb() string {
	switch r {
	case RightRead:
		return "read"
	case RightDeploy:
		return "deploy and restart"
	case RightEnv:
		return "use secrets and data"
	case RightDelete:
		return "delete"
	case RightServers:
		return "manage servers and settings"
	}
	return ""
}

// Has reports whether every right of need is in r.
func (r Rights) Has(need Rights) bool { return r&need == need }

// Missing is the first right of need that r lacks, or 0.
func (r Rights) Missing(need Rights) Rights {
	for _, right := range AllRights {
		if need.Has(right) && !r.Has(right) {
			return right
		}
	}
	return 0
}

// List is the names of the rights, in the order of AllRights.
func (r Rights) List() []string {
	names := []string{}
	for _, right := range AllRights {
		if r.Has(right) {
			names = append(names, right.Name())
		}
	}
	return names
}

// String is the rights as stored: "read,deploy,env".
func (r Rights) String() string { return strings.Join(r.List(), ",") }

// ParseRights reads rights by their names, stored with commas or sent as a
// list. Read is always in. ok is false for a name that is no right.
func ParseRights(names ...string) (Rights, bool) {
	rights := RightRead
	for _, joined := range names {
		for name := range strings.SplitSeq(joined, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			index := slices.IndexFunc(AllRights, func(right Rights) bool { return right.Name() == name })
			if index < 0 {
				return 0, false
			}
			rights |= AllRights[index]
		}
	}
	return rights, true
}

// Scan reads rights from the database.
func (r *Rights) Scan(value any) error {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	}
	rights, _ := ParseRights(text)
	*r = rights
	return nil
}

// Value stores rights as their names: "read,deploy,env".
func (r Rights) Value() (driver.Value, error) { return r.String(), nil }
