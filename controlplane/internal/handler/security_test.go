package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A switch nobody flipped keeps what the server has now, so a page opened
// before a change elsewhere does not undo it.
func TestFlippedKeepsChangesMadeElsewhere(t *testing.T) {
	cases := []struct {
		form string
		now  bool
		want bool
	}{
		{"firewall=1&was_firewall=1", false, false}, // off elsewhere, not flipped here
		{"was_firewall=0", true, true},              // on elsewhere, not flipped here
		{"firewall=1&was_firewall=0", false, true},  // flipped on here
		{"was_firewall=1", true, false},             // flipped off here
		{"firewall=1", false, true},                 // an old form without was_
	}
	for _, c := range cases {
		request := httptest.NewRequest("POST", "/", strings.NewReader(c.form))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := flipped(request, "firewall", c.now); got != c.want {
			values, _ := url.ParseQuery(c.form)
			t.Errorf("flipped(%v, now %v) = %v, want %v", values, c.now, got, c.want)
		}
	}
}
