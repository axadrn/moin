// Package find matches what someone types against names, the same in the
// web palette and the terminal UI's, so both find the same things in the
// same order.
//
// Every word of the query must match a word of the name, in any order. A
// word matches, best first, when a word of the name starts with it, holds
// it, holds its letters in order ("dply" in deploy), or is one typo away
// from it ("deplyo", "setings"; for words of four letters or more).
package find

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Match is how well a query matches a name: Score orders the matches,
// higher first, and Positions are the runes of the name that matched, for
// highlighting.
type Match struct {
	Score     int
	Positions []int
}

// The score of one query word, by how it matched.
const (
	scoreWhole  = 120
	scorePrefix = 100
	scoreInside = 70
	scoreLoose  = 40
	scoreTypo   = 30
	scoreAcross = 20
)

// Name matches query against name. ok is false when a word of the query
// matches nothing. An empty query matches everything with score 0.
func Name(query, name string) (Match, bool) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return Match{}, true
	}
	lower := strings.ToLower(name)
	parts := split(lower)
	if m, ok := byWords(words, parts); ok {
		return m, true
	}
	// One word may also be the letters of several, in order: "sl" for
	// shop logs.
	if len(words) == 1 {
		if positions, ok := loose(words[0], lower); ok {
			return Match{Score: scoreAcross - gaps(positions)/4, Positions: positions}, true
		}
	}
	return Match{}, false
}

// byWords matches each query word against its own word of the name.
func byWords(words []string, parts []part) (Match, bool) {
	used := make([]bool, len(parts))
	var total Match
	last := -1
	for _, word := range words {
		best, bestScore := -1, 0
		var bestPositions []int
		for i, part := range parts {
			if used[i] {
				continue
			}
			score, positions := matchWord(word, part.text)
			if score > bestScore {
				best, bestScore = i, score
				bestPositions = shift(positions, part.at)
			}
		}
		if best < 0 {
			return Match{}, false
		}
		used[best] = true
		total.Score += bestScore
		// Words in the order of the name win a tie.
		if best > last {
			total.Score += 2
		}
		last = best
		total.Positions = append(total.Positions, bestPositions...)
	}
	// Of two names that match as well, the shorter is closer.
	total.Score -= len(parts)
	return total, true
}

type part struct {
	text string
	at   int // the rune index the word starts at in the name
}

// split cuts a name into its words at spaces and the marks between words
// in names: - _ . / › ·
func split(name string) []part {
	var parts []part
	start, at := -1, 0
	for i, r := range []rune(name) {
		if unicode.IsSpace(r) || strings.ContainsRune("-_./›·:,", r) {
			if start >= 0 {
				parts = append(parts, part{string([]rune(name)[start:i]), start})
				start = -1
			}
		} else if start < 0 {
			start = i
		}
		at = i
	}
	if start >= 0 {
		parts = append(parts, part{string([]rune(name)[start : at+1]), start})
	}
	return parts
}

// matchWord is how well a query word matches a word of the name.
func matchWord(word, text string) (int, []int) {
	n := utf8.RuneCountInString(word)
	switch {
	case word == text:
		return scoreWhole + n, span(0, n)
	case strings.HasPrefix(text, word):
		return scorePrefix + n, span(0, n)
	case strings.Contains(text, word):
		return scoreInside + n, span(utf8.RuneCountInString(text[:strings.Index(text, word)]), n)
	}
	if positions, ok := loose(word, text); ok {
		return scoreLoose + n - gaps(positions), positions
	}
	if n >= 4 && typo(word, text) {
		return scoreTypo + n, span(0, utf8.RuneCountInString(text))
	}
	return 0, nil
}

// loose finds the letters of word in text in order, the first one at the
// start of text. Spaces in text are skipped over.
func loose(word, text string) ([]int, bool) {
	w, t := []rune(word), []rune(text)
	if len(w) < 2 || len(t) == 0 || w[0] != t[0] {
		return nil, false
	}
	positions := make([]int, 0, len(w))
	j := 0
	for i := 0; i < len(t) && j < len(w); i++ {
		if t[i] == w[j] && !unicode.IsSpace(t[i]) {
			positions = append(positions, i)
			j++
		}
	}
	return positions, j == len(w)
}

// gaps counts the letters skipped between matched ones.
func gaps(positions []int) int {
	n := 0
	for i := 1; i < len(positions); i++ {
		n += positions[i] - positions[i-1] - 1
	}
	return n
}

// typo is whether word is one edit from text or from a start of text: a
// letter more, less, changed, or two neighbours swapped.
func typo(word, text string) bool {
	w, t := []rune(word), []rune(text)
	if len(t) > len(w)+1 {
		t = t[:len(w)+1]
	}
	if distance(w, t) <= 1 || distance(w, t[:min(len(t), len(w))]) <= 1 {
		return true
	}
	return false
}

// distance is the optimal string alignment distance of a and b.
func distance(a, b []rune) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}

func span(from, n int) []int {
	positions := make([]int, n)
	for i := range positions {
		positions[i] = from + i
	}
	return positions
}

func shift(positions []int, by int) []int {
	out := make([]int, len(positions))
	for i, p := range positions {
		out[i] = p + by
	}
	return out
}
