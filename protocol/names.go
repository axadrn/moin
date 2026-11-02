package protocol

import (
	"regexp"
	"strings"
	"time"
)

// SnapshotIDLayout is the time at the start of every snapshot ID.
const SnapshotIDLayout = "20060102T150405Z"

// snapshotSuffix is the reason after the time, like -before-sql or
// -before-sql-2: lowercase letters, digits and dashes.
var snapshotSuffix = regexp.MustCompile(`^(-[a-z0-9-]{0,64})?$`)

// ValidSnapshotID reports whether id looks like an ID the agent makes:
// 20060102T150405Z, then an optional reason. Anything else never reaches
// the bucket as part of a key.
func ValidSnapshotID(id string) bool {
	if len(id) < len(SnapshotIDLayout) {
		return false
	}
	if _, err := time.Parse(SnapshotIDLayout, id[:len(SnapshotIDLayout)]); err != nil {
		return false
	}
	return snapshotSuffix.MatchString(id[len(SnapshotIDLayout):])
}

// MaxRefLength is the longest branch, tag or commit name ops accepts.
const MaxRefLength = 255

// ValidRefName reports whether name is a branch, tag or commit name by
// git's rules (git check-ref-format), plus two of ops: at most 255 bytes,
// and no leading dash, so a name is never read as a command line flag.
func ValidRefName(name string) bool {
	if name == "" || len(name) > MaxRefLength || name == "@" {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") {
		return false
	}
	if strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
