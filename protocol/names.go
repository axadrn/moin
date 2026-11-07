package protocol

import (
	"regexp"
	"strings"
	"time"
)

// appName is the name of an app: 1 to 63 lowercase letters, digits or
// dashes, starting with a letter or digit. The agent names its directory
// and Docker project after it.
var appName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidAppName reports whether name is a valid app name.
func ValidAppName(name string) bool { return appName.MatchString(name) }

// CopyIDLayout is the ID of a copy, or of the state before a restore:
// the time it was taken.
const CopyIDLayout = "20060102T150405Z"

// ValidCopyID reports whether id is the ID of a copy.
// Anything else never reaches the bucket as part of a key.
func ValidCopyID(id string) bool {
	if len(id) != len(CopyIDLayout) {
		return false
	}
	_, err := time.Parse(CopyIDLayout, id)
	return err == nil
}

// MaxRefLength is the longest branch, tag or commit name moin accepts.
const MaxRefLength = 255

// ValidRefName reports whether name is a branch, tag or commit name by
// git's rules (git check-ref-format), plus two of moin: at most 255 bytes,
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
