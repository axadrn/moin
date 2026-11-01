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
