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
