package protocol

import (
	"reflect"
	"testing"
)

// TestSplitCommand: quotes keep spaces, and nothing a shell would act on
// does anything: it stays text in a word.
func TestSplitCommand(t *testing.T) {
	for _, test := range []struct {
		line string
		want []string
	}{
		{"rails db:migrate", []string{"rails", "db:migrate"}},
		{`psql -U shop -c "select count(*) from orders"`, []string{"psql", "-U", "shop", "-c", "select count(*) from orders"}},
		{`echo 'a b'  "c d"`, []string{"echo", "a b", "c d"}},
		{`echo $HOME; rm -rf / | cat && x`, []string{"echo", "$HOME;", "rm", "-rf", "/", "|", "cat", "&&", "x"}},
		{`echo a\ b`, []string{"echo", "a b"}},
		{`echo "it's"`, []string{"echo", "it's"}},
		{`echo ''`, []string{"echo", ""}},
		{"  ls   /data  ", []string{"ls", "/data"}},
	} {
		got, err := SplitCommand(test.line)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("SplitCommand(%q) = %q, %v; want %q", test.line, got, err, test.want)
		}
	}
	for _, bad := range []string{"", "   ", `echo "open`, `echo 'open`, `echo end\`} {
		if _, err := SplitCommand(bad); err == nil {
			t.Errorf("SplitCommand(%q) took it", bad)
		}
	}
}

// TestJoinCommand: what JoinCommand writes, SplitCommand reads back.
func TestJoinCommand(t *testing.T) {
	for _, argv := range [][]string{
		{"rails", "db:migrate"},
		{"sh", "-c", "echo it's $HOME | wc -c"},
		{"echo", ""},
		{"psql", "-c", `select "a" from b`},
	} {
		got, err := SplitCommand(JoinCommand(argv))
		if err != nil || !reflect.DeepEqual(got, argv) {
			t.Errorf("round trip of %q via %q = %q, %v", argv, JoinCommand(argv), got, err)
		}
	}
}
