package pages

import (
	"fmt"

	"github.com/axadrn/moin/cloud/internal/brand"
	"github.com/axadrn/moin/cloud/internal/compare"
	"github.com/axadrn/moin/cloud/internal/ui/components"
)

// compareTable is a tool's page as one Compare table, grouped: this
// product first, featured, then the tool, each answer with its words, its
// line and where it is written.
func compareTable(competitor compare.Competitor) components.CompareProps {
	ours := components.CompareColumn{Name: brand.Name, Featured: true}
	theirs := components.CompareColumn{Name: competitor.Name}
	var p components.CompareProps
	for i, row := range compare.Rows {
		p.Rows = append(p.Rows, row.Label)
		p.Groups = append(p.Groups, row.Group)
		ours.Cells = append(ours.Cells, compareCell(row.Ours))
		theirs.Cells = append(theirs.Cells, compareCell(competitor.Cells[i]))
	}
	p.Columns = []components.CompareColumn{ours, theirs}
	return p
}

func compareCell(cell compare.Cell) components.CompareCell {
	return components.CompareCell{Mark: string(cell.Mark), Text: cell.Short, Note: cell.Detail, Href: cell.Source}
}

// compareSources is the line under the table: the score, where the facts
// come from and when they were read.
func compareSources(competitor compare.Competitor) string {
	return fmt.Sprintf("%s. From %s's own docs, read %s. Every answer links to where it is written. \"Not documented\" and a line without a mark count for neither side.", competitor.ScoreLine(), competitor.Name, competitor.Read)
}
