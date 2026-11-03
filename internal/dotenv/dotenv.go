// Package dotenv reads and writes the env vars of an app as one .env text,
// KEY=value per line, and says what replacing them with such a text changes.
// The CLI, the TUI and the control plane share it, so a text means the same
// everywhere.
package dotenv

import (
	"fmt"
	"slices"
	"strings"
)

// Parse reads a .env text. Empty lines and lines starting with # are skipped,
// "export " in front of a key is allowed. A value may be bare, in single
// quotes (taken as it is) or in double quotes, where \n, \", \\ and \$
// stand for a line break, a quote, a backslash and a dollar.
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
			return nil, fmt.Errorf("line %d is not KEY=value: %s", number+1, clip(line))
		}
		if _, twice := env[key]; twice {
			return nil, fmt.Errorf("line %d sets %s a second time", number+1, key)
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

// Format writes env as a .env text, sorted by key. Parse reads it back as it
// was.
func Format(env map[string]string) string {
	var out strings.Builder
	for _, key := range Keys(env) {
		fmt.Fprintf(&out, "%s=%s\n", key, Quote(env[key]))
	}
	return out.String()
}

// Quote puts a value in double quotes when it needs them.
func Quote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\"'\\$#") {
		return value
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "$", `\$`)
	return `"` + replacer.Replace(value) + `"`
}

// Keys are the keys of env, sorted.
func Keys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// Change is what replacing the env vars with others does, by key, sorted.
type Change struct {
	Added   []string `json:"added"`
	Changed []string `json:"changed"`
	Removed []string `json:"removed"`
}

// Diff compares the env vars there are with the ones that replace them.
func Diff(current, next map[string]string) Change {
	change := Change{Added: []string{}, Changed: []string{}, Removed: []string{}}
	for _, key := range Keys(next) {
		value, ok := current[key]
		switch {
		case !ok:
			change.Added = append(change.Added, key)
		case value != next[key]:
			change.Changed = append(change.Changed, key)
		}
	}
	for _, key := range Keys(current) {
		if _, ok := next[key]; !ok {
			change.Removed = append(change.Removed, key)
		}
	}
	return change
}

// None reports whether the change changes nothing.
func (c Change) None() bool {
	return len(c.Added)+len(c.Changed)+len(c.Removed) == 0
}

// String says the change in one line: "adds A, changes B, removes C".
func (c Change) String() string {
	var parts []string
	for _, part := range []struct {
		verb string
		keys []string
	}{{"adds", c.Added}, {"changes", c.Changed}, {"removes", c.Removed}} {
		if len(part.keys) > 0 {
			parts = append(parts, part.verb+" "+strings.Join(part.keys, ", "))
		}
	}
	if len(parts) == 0 {
		return "changes nothing"
	}
	return strings.Join(parts, "; ")
}

func clip(line string) string {
	if len(line) > 40 {
		return line[:40] + "…"
	}
	return line
}
