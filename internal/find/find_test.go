package find

import (
	"slices"
	"sort"
	"testing"
)

func TestNameFindsWordsInAnyOrderWithTypos(t *testing.T) {
	for _, c := range []struct {
		query, name string
		ok          bool
	}{
		{"", "shop", true},
		{"shop", "shop", true},
		{"sh", "shop", true},
		{"ploy", "Deploy shop", true},
		{"dply", "Deploy shop", true},
		{"deplyo", "Deploy shop", true},
		{"setings", "shop › Settings", true},
		{"shop set", "shop › Settings", true},
		{"set shop", "shop › Settings", true},
		{"restart shop", "Restart shop", true},
		{"logs api", "shop › Logs", false},
		{"sl", "shop logs", true},
		{"pohs", "shop", false},
		{"xyz", "Deploy shop", false},
		// Short words need to be right: "dp" is no typo of "db".
		{"dp", "db", false},
		// Each word of the query takes its own word of the name.
		{"shop shop", "shop › Settings", false},
	} {
		if _, ok := Name(c.query, c.name); ok != c.ok {
			t.Errorf("Name(%q, %q) ok = %v, want %v", c.query, c.name, ok, c.ok)
		}
	}
}

func TestNameRanksTheCloserMatchFirst(t *testing.T) {
	names := []string{"Restart shop", "shop", "shop › Settings", "Settings", "workshop"}
	rank := func(query string) []string {
		type hit struct {
			name  string
			score int
		}
		var hits []hit
		for _, name := range names {
			if m, ok := Name(query, name); ok {
				hits = append(hits, hit{name, m.Score})
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		out := []string{}
		for _, h := range hits {
			out = append(out, h.name)
		}
		return out
	}
	if got := rank("shop"); got[0] != "shop" || slices.Index(got, "workshop") < slices.Index(got, "Restart shop") {
		t.Fatalf("shop ranks %v: the app first, a word that only holds it last", got)
	}
	if got := rank("settings"); got[0] != "Settings" {
		t.Fatalf("settings ranks %v", got)
	}
	if got := rank("setings"); len(got) != 2 {
		t.Fatalf("a typo still finds both settings: %v", got)
	}
}

func TestNameTellsWhatMatched(t *testing.T) {
	m, ok := Name("sl", "shop logs")
	if !ok || !slices.Equal(m.Positions, []int{0, 5}) {
		t.Fatalf("sl across shop logs = %v, %v", m.Positions, ok)
	}
	if exact, _ := Name("logs", "shop logs"); exact.Score <= m.Score {
		t.Fatalf("a whole word scores above letters across words: %d <= %d", exact.Score, m.Score)
	}
	m, ok = Name("sh lo", "shop logs")
	if !ok || !slices.Equal(m.Positions, []int{0, 1, 5, 6}) {
		t.Fatalf("positions of sh lo in shop logs = %v, %v", m.Positions, ok)
	}
}
