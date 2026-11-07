package service

import (
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
)

func TestNextServerNameTakesTheLowestFreeNumber(t *testing.T) {
	for _, test := range []struct {
		names []string
		want  string
	}{
		{nil, "web-1"},
		{[]string{"web-1"}, "web-2"},
		{[]string{"web-1", "web-3"}, "web-2"},
		{[]string{"prod", "web-2", "web-1"}, "web-3"},
	} {
		var servers []model.Server
		for _, name := range test.names {
			servers = append(servers, model.Server{Name: name})
		}
		if got := NextServerName(servers); got != test.want {
			t.Errorf("NextServerName(%v) = %s, want %s", test.names, got, test.want)
		}
	}
}
