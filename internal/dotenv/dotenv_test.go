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

// What Format writes, Parse reads back the same.
func TestRoundTrip(t *testing.T) {
	env := map[string]string{"PLAIN": "abc", "SPACE": "a b", "QUOTES": `"'`, "LINES": "a\nb\tc", "DOLLAR": "$X", "HASH": "a #b", "EMPTY": "", "SLASH": `a\b`}
	back, err := Parse(Format(env))
	if err != nil || !maps.Equal(back, env) {
		t.Errorf("round trip = %q, %v", back, err)
	}
}

func TestDiff(t *testing.T) {
	change := Diff(map[string]string{"KEEP": "1", "EDIT": "1", "GONE": "1"}, map[string]string{"KEEP": "1", "EDIT": "2", "NEW": "1"})
	if strings.Join(change.Added, ",") != "NEW" || strings.Join(change.Changed, ",") != "EDIT" || strings.Join(change.Removed, ",") != "GONE" {
		t.Errorf("Diff = %+v", change)
	}
	if change.String() != "adds NEW; changes EDIT; removes GONE" {
		t.Errorf("String = %q", change)
	}
	if !Diff(map[string]string{"A": "1"}, map[string]string{"A": "1"}).None() {
		t.Error("the same env vars change something")
	}
}
