// Package link reads the IDs people name things by. A link names a thing
// by its name; the API, the CLI and MCP take its ID too, shown as the UUID
// without dashes.
package link

import "strings"

// Size is the length of an ID as people see it.
const Size = 32

// ID is the ID people see of a UUID: without dashes, 32 characters.
func ID(uuid string) string { return strings.ReplaceAll(uuid, "-", "") }

// Parse finds the UUID an ID names: 32 characters, or a UUID with its
// dashes. It returns the UUID with its dashes, the form the database
// keeps, and false for anything else, such as a name.
func Parse(ref string) (string, bool) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if len(ref) == 36 && ref[8] == '-' && ref[13] == '-' && ref[18] == '-' && ref[23] == '-' {
		ref = ID(ref)
	}
	if len(ref) != Size {
		return "", false
	}
	for _, character := range ref {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return "", false
		}
	}
	return ref[:8] + "-" + ref[8:12] + "-" + ref[12:16] + "-" + ref[16:20] + "-" + ref[20:], true
}
