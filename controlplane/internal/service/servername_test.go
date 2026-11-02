package service

import (
	"regexp"
	"testing"
)

func TestNameWordsAreShortLowercaseAndUnique(t *testing.T) {
	word := regexp.MustCompile(`^[a-z]{2,9}$`)
	for _, list := range [][]string{nameAdjectives, nameNouns} {
		seen := map[string]bool{}
		for _, w := range list {
			if !word.MatchString(w) || seen[w] {
				t.Errorf("word %q is not short lowercase ASCII or comes twice", w)
			}
			seen[w] = true
		}
		if len(list) < 200 {
			t.Errorf("only %d words", len(list))
		}
	}
}
