package dotenv

import (
	"maps"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	env, err := Parse("# comment\n\nexport A=1\nB = two words # note\nC=\"line\\none \\\"q\\\" \\$HOME\"\nD='$raw \\n'\nE=\r\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "two words", "C": "line\none \"q\" $HOME", "D": `$raw \n`, "E": ""}
	if !maps.Equal(env, want) {
		t.Errorf("Parse = %q, want %q", env, want)
	}
	for _, bad := range []string{"NOEQUALS", "=1", "A=1\nA=2", `A="open`, "A='open"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) took it", bad)
		}
	}
	if _, err := Parse("A=1\nA=2"); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("a key twice says %v", err)
	}
}

// Diff needs only the keys there are: every key that comes again is
// replaced, whatever its value was.
func TestDiff(t *testing.T) {
	change := Diff([]string{"KEEP", "GONE"}, map[string]string{"KEEP": "1", "NEW": "1"})
	if strings.Join(change.Added, ",") != "NEW" || strings.Join(change.Replaced, ",") != "KEEP" || strings.Join(change.Removed, ",") != "GONE" {
		t.Errorf("Diff = %+v", change)
	}
	if change.String() != "adds NEW; replaces KEEP; removes GONE" {
		t.Errorf("String = %q", change)
	}
	if !Diff(nil, map[string]string{}).None() {
		t.Error("no env vars replaced by none change something")
	}
}
