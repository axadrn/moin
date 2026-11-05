package tui

// The look: one palette for dark and one for light terminals, and the few
// shapes every view is drawn with. Colour carries status only, like the web
// app (controlplane/DESIGN.md): green live, amber running, red failed.

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

// palette is the colours of one terminal background.
type palette struct {
	Fg, Muted, Faint, Line color.Color
	Good, Warn, Bad        color.Color
	SelBg, Raised, Float   color.Color
	// mono is a terminal without colour, like NO_COLOR asks: a selected
	// row gets a mark instead of its band.
	mono bool
}

var (
	dark = palette{
		Fg: hex("#fafafa"), Muted: hex("#a1a1aa"), Faint: hex("#5e5e66"), Line: hex("#3f3f46"),
		Good: hex("#4ade80"), Warn: hex("#fbbf24"), Bad: hex("#f87171"),
		SelBg: hex("#27272a"), Raised: hex("#18181b"), Float: hex("#323238"),
	}
	light = palette{
		Fg: hex("#09090b"), Muted: hex("#5b5b63"), Faint: hex("#a1a1aa"), Line: hex("#d4d4d8"),
		Good: hex("#15803d"), Warn: hex("#b45309"), Bad: hex("#dc2626"),
		SelBg: hex("#e4e4e7"), Raised: hex("#f4f4f5"), Float: hex("#e4e4e7"),
	}
)

func hex(s string) color.Color { return lipgloss.Color(s) }

const (
	dot    = "●"
	dotOff = "○"
	sep    = " › "
	pad    = 3 // inside a panel, left and right: a space and the gutter
)

var sparks = []rune("▁▂▃▄▅▆▇█")

// ui draws with one palette. float is set inside overlays, which sit on
// their own background.
type ui struct {
	p     palette
	float bool
}

func sty() lipgloss.Style { return lipgloss.NewStyle() }

func (u ui) fg(c color.Color, s string) string { return sty().Foreground(c).Render(s) }
func (u ui) bold(c color.Color, s string) string {
	return sty().Foreground(c).Bold(true).Render(s)
}
func (u ui) text(s string) string   { return u.fg(u.p.Fg, s) }
func (u ui) muted(s string) string  { return u.fg(u.p.Muted, s) }
func (u ui) faint(s string) string  { return u.fg(u.p.Faint, s) }
func (u ui) strong(s string) string { return u.bold(u.p.Fg, s) }
func (u ui) good(s string) string   { return u.fg(u.p.Good, s) }
func (u ui) warn(s string) string   { return u.fg(u.p.Warn, s) }
func (u ui) bad(s string) string    { return u.fg(u.p.Bad, s) }

// tone is the one place a status word gets its colour.
func tone(word string) int {
	switch word {
	case "live", "online", "up", "ok", "on", "succeeded", "passed", "checked", "points":
		return toneGood
	case "deploying", "running", "waiting", "pending", "update", "slow", "restoring", "starting":
		return toneWarn
	case "failed", "error", "offline", "down", "unreachable":
		return toneBad
	}
	return toneIdle
}

const (
	toneIdle = iota
	toneGood
	toneWarn
	toneBad
)

func (u ui) color(word string) color.Color {
	switch tone(word) {
	case toneGood:
		return u.p.Good
	case toneWarn:
		return u.p.Warn
	case toneBad:
		return u.p.Bad
	}
	return u.p.Muted
}

// dot is the status dot; off draws it hollow, for the pulse.
func (u ui) dot(word string, on bool) string {
	g := dot
	if !on || tone(word) == toneIdle {
		g = dotOff
	}
	return u.fg(u.color(word), g)
}

// chip is a status: the dot and the word, both in the status colour, so a
// status reads the same in every view.
func (u ui) chip(word, label string) string {
	if tone(word) == toneIdle {
		return u.muted(dotOff + " " + label)
	}
	return u.fg(u.color(word), dot+" "+label)
}

func (u ui) spark(vals []float64, w int, c color.Color) string {
	if len(vals) > w {
		vals = vals[len(vals)-w:]
	}
	if len(vals) == 0 {
		return u.faint(strings.Repeat("·", min(w, 3)))
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		lo, hi = min(lo, v), max(hi, v)
	}
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if hi > lo {
			i = int((v-lo)/(hi-lo)*float64(len(sparks)-1) + 0.5)
		} else if hi > 0 {
			i = len(sparks) / 2
		}
		b.WriteRune(sparks[max(0, min(i, len(sparks)-1))])
	}
	return u.fg(c, b.String())
}

// keys renders key hints: the key bright, what it does muted.
func (u ui) keys(ks ...kb) string {
	var parts []string
	for _, k := range ks {
		parts = append(parts, u.strong(k.k)+" "+u.muted(k.l))
	}
	return strings.Join(parts, u.faint(" · "))
}

type kb struct{ k, l string }

// width helpers

func wid(s string) int { return lipgloss.Width(s) }

// truncate cuts s to w cells, with an ellipsis when it cuts.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if wid(s) <= w {
		return s
	}
	return sty().MaxWidth(w-1).Render(s) + "…"
}

func padR(s string, w int) string {
	n := wid(s)
	if n > w {
		return truncate(s, w)
	}
	return s + strings.Repeat(" ", w-n)
}

func padL(s string, w int) string {
	n := wid(s)
	if n > w {
		return truncate(s, w)
	}
	return strings.Repeat(" ", w-n) + s
}

func center(s string, w int) string {
	n := wid(s)
	if n >= w {
		return truncate(s, w)
	}
	l := (w - n) / 2
	return strings.Repeat(" ", l) + s + strings.Repeat(" ", w-n-l)
}

// spread puts l and r on one line of width w, r flush right.
func spread(l, r string, w int) string {
	gap := w - wid(l) - wid(r)
	if gap < 1 {
		if wid(l) >= w {
			return truncate(l, w)
		}
		return l + strings.Repeat(" ", w-wid(l))
	}
	return l + strings.Repeat(" ", gap) + r
}

func bgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// fill pads every line to w and paints c behind all of it, also behind
// text other styles drew, by opening the background again after every
// reset.
func fill(s string, c color.Color, w int) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		l = padR(l, w)
		if c != nil {
			bg := bgSeq(c)
			l = strings.ReplaceAll(l, "\x1b[0m", "\x1b[m")
			l = bg + strings.ReplaceAll(l, "\x1b[m", "\x1b[m"+bg) + "\x1b[m"
		}
		ls[i] = l
	}
	return strings.Join(ls, "\n")
}

// block fits s to exactly w by h.
func block(s string, w, h int) []string {
	ls := strings.Split(s, "\n")
	if len(ls) > h {
		ls = ls[:h]
	}
	for len(ls) < h {
		ls = append(ls, "")
	}
	for i := range ls {
		ls[i] = padR(ls[i], w)
	}
	return ls
}

func lines(s string) []string { return strings.Split(s, "\n") }

// hjoin puts blocks side by side with a gap.
func hjoin(gap int, parts ...string) string {
	h := 0
	for _, p := range parts {
		h = max(h, len(lines(p)))
	}
	out := make([]string, h)
	for i, p := range parts {
		ls := lines(p)
		w := 0
		for _, l := range ls {
			w = max(w, wid(l))
		}
		for y := range h {
			l := ""
			if y < len(ls) {
				l = ls[y]
			}
			if i > 0 {
				out[y] += strings.Repeat(" ", gap)
			}
			out[y] += padR(l, w)
		}
	}
	return strings.Join(out, "\n")
}

func vjoin(parts ...string) string { return strings.Join(parts, "\n") }

// overlay draws pop over base at x, y.
func overlay(base []string, pop string, x, y int) []string {
	c := lipgloss.NewCompositor(
		lipgloss.NewLayer(strings.Join(base, "\n")),
		lipgloss.NewLayer(pop).X(max(0, x)).Y(max(0, y)).Z(1),
	)
	return lines(c.Render())
}

// panel is a rounded box of exactly w by h with the title set into the top
// edge and a note flush right in it.
func (u ui) panel(title, right string, body []string, w, h int, focus bool) string {
	b := lipgloss.RoundedBorder()
	bc := u.p.Line
	if focus {
		bc = u.p.Muted
	}
	iw := inner(w)
	edge := func(s string) string { return u.fg(bc, s) }
	head := ""
	if title != "" {
		head = u.strong(" " + title + " ")
	}
	rh := ""
	if right != "" {
		rh = " " + right + " "
	}
	if wid(head)+wid(rh)+4 > w {
		rh = ""
	}
	if wid(head)+4 > w {
		head = truncate(head, max(0, w-4))
	}
	n := max(0, w-4-wid(head)-wid(rh))
	top := edge(b.TopLeft+b.Top) + head + edge(strings.Repeat(b.Top, n)) + rh + edge(b.Top+b.TopRight)
	out := []string{top}
	for i := range max(0, h-2) {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		// a selected row paints the gutters with its band, or without
		// colour carries a mark in the left one
		gl, gr := "  ", "  "
		if strings.HasPrefix(l, "\x1b[48;") {
			if end := strings.IndexByte(l, 'm'); end > 0 {
				gl = l[:end+1] + "  \x1b[m"
				gr = gl
			}
			if u.p.mono {
				gl = u.strong("› ")
			}
		}
		out = append(out, edge(b.Left)+" "+gl+padR(l, iw)+gr+" "+edge(b.Right))
	}
	out = append(out, edge(b.BottomLeft+strings.Repeat(b.Bottom, max(0, w-2))+b.BottomRight))
	return strings.Join(out, "\n")
}

// inner is the width of a panel's body: inside the border, a space and a
// gutter of two on each side. The gutters carry the band of a selected
// row, so rows and text line up.
func inner(w int) int { return max(1, w-2-2*pad) }

// chrome is how many lines a panel adds around its body.
const chrome = 2

type cell struct {
	s     string
	w     int // 0 takes what is left
	right bool
}

// row lays cells out in columns; a selected row sits on a soft band that
// the panel carries into its gutters.
func (u ui) row(cells []cell, w int, sel bool) string {
	var b strings.Builder
	used := 0
	for i, c := range cells {
		if i > 0 {
			b.WriteString(" ")
			used++
		}
		cw := c.w
		if cw <= 0 {
			cw = max(0, w-used)
		}
		if c.right {
			b.WriteString(padL(c.s, cw))
		} else {
			b.WriteString(padR(c.s, cw))
		}
		used += cw
	}
	body := padR(b.String(), w)
	if !sel {
		return body
	}
	bg := u.p.SelBg
	if u.float {
		bg = u.p.Float
	}
	return fill(body, bg, w)
}

// head is a column header cell.
func (u ui) head(s string, w int, right bool) cell { return cell{u.faint(s), w, right} }

func (u ui) rule(w int) string { return u.fg(u.p.Line, strings.Repeat("─", max(0, w))) }

// clean drops terminal escapes and control characters from text that came
// from outside, such as container logs, so it cannot break the layout.
func clean(s string) string {
	s = escapes.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

var escapes = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// wrap breaks s into lines of at most w cells, at spaces.
func wrap(s string, w int) []string {
	var out []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case wid(cur)+1+wid(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
