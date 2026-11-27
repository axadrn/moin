package brand

import "testing"

// Search engines cut a description after about 160 characters.
func TestDescriptionFitsSearch(t *testing.T) {
	if n := len(Description); n > 160 {
		t.Errorf("Description is %d characters, want at most 160", n)
	}
}
