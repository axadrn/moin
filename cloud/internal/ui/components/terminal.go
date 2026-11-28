package components

import (
	"strconv"

	"github.com/a-h/templ"
)

// terminalDelay is when a line of a playing Terminal shows up: one after
// the other, fast.
func terminalDelay(line int) templ.SafeCSS {
	return templ.SafeCSS("--line:" + strconv.Itoa(line))
}

// barWidth is the share of a bar as CSS: 42%.
func barWidth(share int64) string {
	return strconv.FormatInt(max(0, min(100, share)), 10) + "%"
}
