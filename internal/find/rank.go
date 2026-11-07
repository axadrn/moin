package find

import "sort"

// The order of a palette, the same in the web and the terminal UI.

// recentBoost is what the newest pick gains in Rank; each older pick
// gains a point less, at least one. Enough to win between two matches of
// the same kind, too little to lift "workshop" over "shop" for "shop".
const recentBoost = 20

// Hit is an item that matched the query, and how well.
type Hit[T any] struct {
	Item T
	Match
}

// Rank is every item whose text matches query, the best first. recent is
// where an item stands among the latest picks, 0 for the newest and -1
// for none; a recent pick gains a little, so it wins a tie.
func Rank[T any](query string, items []T, text func(T) string, recent func(T) int) []Hit[T] {
	var hits []Hit[T]
	for _, item := range items {
		m, ok := Name(query, text(item))
		if !ok {
			continue
		}
		if i := recent(item); i >= 0 {
			m.Score += max(1, recentBoost-i)
		}
		hits = append(hits, Hit[T]{item, m})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits
}

// Start is what a palette lists before anything is typed: what goes with
// the page you are on (here), then the recent picks, then the basics (the
// apps and the places), each item once by its key.
func Start[T any](here, recent, basics []T, key func(T) string) []T {
	var out []T
	seen := map[string]bool{}
	for _, list := range [][]T{here, recent, basics} {
		for _, item := range list {
			if !seen[key(item)] {
				seen[key(item)] = true
				out = append(out, item)
			}
		}
	}
	return out
}
