package pages

import "fmt"

// liveCount writes a count with thousands separators: 12,480.
func liveCount(count int) string {
	text := fmt.Sprint(count)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

// The width and type of the catalog pages (/apps), from the home page
// before it moved to the kit. They go when the catalog moves too.
const (
	homeWrap = "mx-auto w-full max-w-[1240px] px-4 sm:px-8"
	typeH2   = "font-semibold tracking-[-0.04em] leading-[1.05] text-[clamp(2rem,4.5vw,3.25rem)] text-balance"
	typeH3   = "text-xl font-semibold tracking-[-0.02em]"
	typeLead = "text-lg leading-8 text-muted-foreground"
	typeText = "text-base leading-7 text-muted-foreground"
)
