package find

import (
	"slices"
	"testing"
)

func same(s string) string { return s }

func TestRankPutsTheBestMatchFirst(t *testing.T) {
	names := []string{"workshop", "Restart shop", "shop", "Settings"}
	none := func(string) int { return -1 }
	got := titles(Rank("shop", names, same, none))
	if want := []string{"shop", "Restart shop", "workshop"}; !slices.Equal(got, want) {
		t.Fatalf("Rank(shop) = %v, want %v", got, want)
	}
	if hits := Rank("sh", names, same, none); !slices.Equal(hits[0].Positions, []int{0, 1}) {
		t.Fatalf("a hit tells what matched: %v", hits[0].Positions)
	}
}

func TestRankLiftsRecentPicks(t *testing.T) {
	names := []string{"shop logs", "shop deploys", "shop domains"}
	recent := []string{"shop domains", "shop deploys"}
	picked := func(name string) int { return slices.Index(recent, name) }
	got := titles(Rank("shop", names, same, picked))
	if want := []string{"shop domains", "shop deploys", "shop logs"}; !slices.Equal(got, want) {
		t.Fatalf("of equal matches the newest pick wins: %v, want %v", got, want)
	}
	// The newest pick still does not beat a better kind of match.
	newest := func(name string) int {
		if name == "workshop" {
			return 0
		}
		return -1
	}
	got = titles(Rank("shop", []string{"workshop", "shop"}, same, newest))
	if want := []string{"shop", "workshop"}; !slices.Equal(got, want) {
		t.Fatalf("a recent pick does not beat a whole word: %v, want %v", got, want)
	}
}

func TestStartListsHereThenRecentThenBasicsOnce(t *testing.T) {
	here := []string{"shop logs", "redeploy shop"}
	recent := []string{"blog", "shop logs"}
	basics := []string{"shop", "blog", "servers"}
	got := Start(here, recent, basics, same)
	if want := []string{"shop logs", "redeploy shop", "blog", "shop", "servers"}; !slices.Equal(got, want) {
		t.Fatalf("Start = %v, want %v", got, want)
	}
}

func titles(hits []Hit[string]) []string {
	out := []string{}
	for _, h := range hits {
		out = append(out, h.Item)
	}
	return out
}
