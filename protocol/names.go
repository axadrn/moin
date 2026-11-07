package protocol

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"
)

// appName is the name of an app: 1 to 63 lowercase letters, digits or
// dashes, starting with a letter or digit. The daemon names its directory
// and Docker project after it.
var appName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidAppName reports whether name is a valid app name.
func ValidAppName(name string) bool { return appName.MatchString(name) }

// CopyIDLayout is the time a copy, or the state before a restore, was
// taken. Its ID is that time and six random digits, like
// 20261107T140000Z-482913: sorted by time, and two of the same second
// still apart. IDs of only the time, from before, stay valid.
const CopyIDLayout = "20060102T150405Z"

var copyID = regexp.MustCompile(`^\d{8}T\d{6}Z(-\d{6})?$`)

// NewCopyID is the ID of a copy taken at at.
func NewCopyID(at time.Time) string {
	return at.UTC().Format(CopyIDLayout) + "-" + fmt.Sprintf("%06d", rand.IntN(1000000))
}

// CopyTime is the time in a copy's ID.
func CopyTime(id string) (time.Time, bool) {
	if !copyID.MatchString(id) {
		return time.Time{}, false
	}
	at, err := time.Parse(CopyIDLayout, id[:len(CopyIDLayout)])
	return at, err == nil
}

// ValidCopyID reports whether id is the ID of a copy.
// Anything else never reaches the bucket as part of a key.
func ValidCopyID(id string) bool {
	_, ok := CopyTime(id)
	return ok
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
