package cli

import (
	"testing"
	"time"
)

// TestSentenceKeepsNames capitalizes a sentence but leaves the name of moin
// and versions as they are.
func TestSentenceKeepsNames(t *testing.T) {
	for text, want := range map[string]string{
		"no SQLite database on a volume of blog": "No SQLite database on a volume of blog",
		"moin cannot read me/blog":               "moin cannot read me/blog",
		"v4 failed":                              "v4 failed",
	} {
		if got := sentence(text); got != want {
			t.Errorf("sentence(%q) = %q, want %q", text, got, want)
		}
	}
}

// TestParseTimeOfToday reads a time without a day as today in UTC.
func TestParseTimeOfToday(t *testing.T) {
	parsed, err := parseTime("14:01", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if parsed.Year() != now.Year() || parsed.YearDay() != now.YearDay() || parsed.Hour() != 14 || parsed.Minute() != 1 {
		t.Fatalf("parseTime(14:01) = %v", parsed)
	}
	parsed, err = parseTime("2026-10-31 14:02:00", time.Now())
	if err != nil || !parsed.Equal(time.Date(2026, 10, 31, 14, 2, 0, 0, time.UTC)) {
		t.Fatalf("parseTime with a day = %v, %v", parsed, err)
	}
}
