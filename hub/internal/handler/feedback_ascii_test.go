package handler

import (
	"encoding/json"
	"testing"
)

func TestASCIIJSONKeepsTheText(t *testing.T) {
	text := "Use https://hooks.slack.com/services/… — “quotes”, ü and 🚀"
	data, _ := json.Marshal(map[string]string{"message": text})
	header := asciiJSON(data)
	for _, r := range header {
		if r > 127 {
			t.Fatalf("header has a rune beyond ASCII: %q in %s", r, header)
		}
	}
	var back map[string]string
	if err := json.Unmarshal([]byte(header), &back); err != nil || back["message"] != text {
		t.Fatalf("read back %q, %v; want %q", back["message"], err, text)
	}
}
