// Package dotenv reads the env vars of an app from one .env text, KEY=value
// per line, writes them as one, and says what replacing them with such a
// text changes. The CLI, the TUI, the hub and the daemon share it, so a text
// means the same everywhere.
package dotenv

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Parse reads a .env text. Empty lines and lines starting with # are
// skipped, "export " in front of a key is allowed. A value may be bare, in
// single quotes (taken as it is) or in double quotes, where \n, \", \\
// and \$ stand for a line break, a quote, a backslash and a dollar; \r for
// a carriage return. An error names the line by its number only, never by
// what is on it: a broken line may hold a secret.
func Parse(text string) (map[string]string, error) {
	env := map[string]string{}
	for number, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("line %d is not KEY=value", number+1)
		}
		if _, twice := env[key]; twice {
			return nil, fmt.Errorf("line %d sets a key a second time", number+1)
		}
		value, err := unquote(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", number+1, err)
		}
		env[key] = value
	}
	return env, nil
}

func unquote(value string) (string, error) {
	switch {
	case strings.HasPrefix(value, "'"):
		end := strings.Index(value[1:], "'")
		if end < 0 {
			return "", fmt.Errorf("the ' is not closed")
		}
		return value[1 : end+1], nil
	case strings.HasPrefix(value, `"`):
		var out strings.Builder
		for index := 1; index < len(value); index++ {
			switch char := value[index]; {
			case char == '"':
				return out.String(), nil
			case char == '\\' && index+1 < len(value):
				index++
				switch next := value[index]; next {
				case 'n':
					out.WriteByte('\n')
				case 'r':
					out.WriteByte('\r')
				case 't':
					out.WriteByte('\t')
				default:
					out.WriteByte(next)
				}
			default:
				out.WriteByte(char)
			}
		}
		return "", fmt.Errorf(`the " is not closed`)
	}
	// A bare value ends where a comment starts.
	if index := strings.Index(value, " #"); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	return value, nil
}

// Format writes env as a .env text Parse reads back exactly, and docker
// compose too: one KEY="value" per line, sorted by key, with \, ", $ and
// line breaks escaped, so compose fills in no ${VAR} of a value.
func Format(env map[string]string) string {
	var text strings.Builder
	for _, key := range slices.Sorted(maps.Keys(env)) {
		text.WriteString(key + `="` + quote(env[key]) + "\"\n")
	}
	return text.String()
}

var quote = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`, "\r", `\r`).Replace

// Change is what replacing the env vars with others does, by key, sorted.
// Replaced are the keys that stay with another value.
type Change struct {
	Added    []string `json:"added"`
	Replaced []string `json:"replaced"`
	Removed  []string `json:"removed"`
}

// Diff compares the env vars there are with those that replace them.
func Diff(old, next map[string]string) Change {
	change := Change{Added: []string{}, Replaced: []string{}, Removed: []string{}}
	for _, key := range slices.Sorted(maps.Keys(next)) {
		value, ok := old[key]
		switch {
		case !ok:
			change.Added = append(change.Added, key)
		case value != next[key]:
			change.Replaced = append(change.Replaced, key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(old)) {
		if _, ok := next[key]; !ok {
			change.Removed = append(change.Removed, key)
		}
	}
	return change
}

// None reports whether the change changes nothing.
func (c Change) None() bool {
	return len(c.Added)+len(c.Replaced)+len(c.Removed) == 0
}

// String says the change in one line: "adds A; replaces B; removes C".
func (c Change) String() string {
	var parts []string
	for _, part := range []struct {
		verb string
		keys []string
	}{{"adds", c.Added}, {"replaces", c.Replaced}, {"removes", c.Removed}} {
		if len(part.keys) > 0 {
			parts = append(parts, part.verb+" "+strings.Join(part.keys, ", "))
		}
	}
	if len(parts) == 0 {
		return "changes nothing"
	}
	return strings.Join(parts, "; ")
}
