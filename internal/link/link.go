// Package link names things in links the way Notion does: <name>-<id>,
// such as my-app-0f8e…, the ID being the UUID without dashes. Only the
// ID counts, so a link keeps working when the thing is renamed, and a
// link with an old or a wrong name still finds it. Workspaces, apps,
// servers and buckets are linked this way, and their ID is shown to
// people in the same 32-character form everywhere.
package link

import "strings"

// Size is the length of an ID as people see it.
const Size = 32

// ID is the ID people see of a UUID: without dashes, 32 characters.
func ID(uuid string) string { return strings.ReplaceAll(uuid, "-", "") }

// To is how a link names a thing: its name in link form, a dash, its ID.
// The name keeps lowercase letters and digits; anything else becomes one
// dash.
func To(name, uuid string) string {
	var out strings.Builder
	dash := false
	for _, character := range strings.ToLower(name) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			if dash {
				out.WriteByte('-')
			}
			out.WriteRune(character)
			dash = false
		} else if out.Len() > 0 {
			dash = true
		}
	}
	switch {
	case out.Len() == 0:
		return ID(uuid)
	case uuid == "":
		return out.String()
	}
	return out.String() + "-" + ID(uuid)
}

// Parse finds the ID a link names: the last 32 characters of
// <name>-<id>, a bare ID of 32 characters or a UUID with dashes. It
// returns the UUID with its dashes, the form the database keeps, and
// false for anything else, such as a plain name.
func Parse(ref string) (string, bool) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if len(ref) == 36 && ref[8] == '-' && ref[13] == '-' && ref[18] == '-' && ref[23] == '-' {
		ref = ID(ref)
	}
	if len(ref) < Size || len(ref) > Size && ref[len(ref)-Size-1] != '-' {
		return "", false
	}
	id := ref[len(ref)-Size:]
	for _, character := range id {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return "", false
		}
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:], true
}
