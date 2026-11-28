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

// homeCompareHead names the columns of the home table: the fact, this
// tool, then the others.
func homeCompareHead() []string {
	head := []string{"", brand.Name}
	for _, competitor := range compare.Competitors {
		head = append(head, competitor.Name)
	}
	return head
}

// homeCompareRows are the facts the home page shows, by their place in
// compare.Rows: who commands the server, what is open to the internet,
// the daily check, and who notices a dead server.
func homeCompareRows() []int {
	return []int{0, 1, 3, 5}
}
