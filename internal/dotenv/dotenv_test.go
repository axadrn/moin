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

// Format writes what Parse reads back, also values with quotes, dollars,
// backslashes and line breaks.
func TestFormatRoundTrip(t *testing.T) {
	env := map[string]string{"A": "plain", "B": "it's \"$5\"\nnow\r\\end", "C": "", "D": "${HOME}"}
	text := Format(env)
	if !strings.HasPrefix(text, "A=\"plain\"\nB=") {
		t.Errorf("Format = %q", text)
	}
	back, err := Parse(text)
	if err != nil || !maps.Equal(back, env) {
		t.Errorf("Parse(Format) = %q, %v; want %q", back, err, env)
	}
}

// TestParseErrorsNameOnlyTheLine: a broken line may hold a secret, and the
// error travels to the hub: it says the line number, never the text.
func TestParseErrorsNameOnlyTheLine(t *testing.T) {
	for _, text := range []string{"hunter2-secret", "OK=1\nhunter2-secret", "A='hunter2-secret", "A=\"hunter2-secret", "A=hunter2-secret\nA=hunter2-secret"} {
		_, err := Parse(text)
		if err == nil || strings.Contains(err.Error(), "hunter2") || !strings.HasPrefix(err.Error(), "line ") {
			t.Errorf("Parse(%q) = %v", text, err)
		}
	}
}
