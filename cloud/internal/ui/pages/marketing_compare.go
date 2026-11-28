package pages

import (
	"fmt"
	"strings"

	"github.com/axadrn/moin/cloud/internal/brand"
	"github.com/axadrn/moin/cloud/internal/compare"
	"github.com/axadrn/moin/cloud/internal/ui/components"
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

// homeCompare is the home table: this tool, then the others, each with
// its answer to the facts of homeCompareRows.
func homeCompare() components.CompareProps {
	rows := homeCompareRows()
	p := components.CompareProps{}
	for _, row := range rows {
		p.Rows = append(p.Rows, compare.Rows[row].Label)
	}
	column := func(name, href string, featured bool, cell func(row int) compare.Cell) components.CompareColumn {
		col := components.CompareColumn{Name: name, Href: href, Featured: featured}
		for _, row := range rows {
			c := cell(row)
			col.Cells = append(col.Cells, components.CompareCell{Mark: string(c.Mark), Href: c.Source, Title: c.Short})
		}
		return col
	}
	p.Columns = append(p.Columns, column(brand.Name, "", true, func(row int) compare.Cell { return compare.Rows[row].Ours }))
	for _, competitor := range compare.Competitors {
		p.Columns = append(p.Columns, column(competitor.Name, "/compare/"+competitor.Slug, false, func(row int) compare.Cell { return competitor.Cells[row] }))
	}
	return p
}

// homeCompareRows are the facts the home page shows, by their place in
// compare.Rows: who commands the server, what is open to the internet,
// the daily check, and who notices a dead server.
func homeCompareRows() []int {
	return []int{0, 1, 8, 10}
}
