package pages

import (
	"fmt"
	"strings"

	"github.com/axadrn/moin/cloud/internal/brand"
	"github.com/axadrn/moin/cloud/internal/compare"
)

// compareIndexDescription names the first tools and counts the rest, for
// /compare in search.
func compareIndexDescription() string {
	names := make([]string, 0, 3)
	for _, competitor := range compare.Competitors[:3] {
		names = append(names, competitor.Name)
	}
	return fmt.Sprintf("%s next to %s and %d more tools. Fact by fact, from each tool's own docs.", brand.Name, strings.Join(names, ", "), len(compare.Competitors)-len(names))
}

// homeCompareRows are the facts the home page shows, by their place in
// compare.Rows: who commands the server, the daily check, who notices a
// dead server, and several servers, where others do more.
func homeCompareRows() []int {
	return []int{0, 3, 5, 8}
}

// markSymbol is how a mark reads in the home table.
func markSymbol(mark compare.Mark) string {
	switch mark {
	case compare.Yes:
		return "✓"
	case compare.Part:
		return "◐"
	case compare.No:
		return "✗"
	}
	return "·"
}
