package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The list form sends the names in their order on the page; an arrow
// button moves its item by one, and not past either end.
func TestFormOrder(t *testing.T) {
	for _, test := range []struct {
		form url.Values
		want string
	}{
		{url.Values{"order": {"a", "b", "c"}}, "a,b,c"},
		{url.Values{"order": {"a", "b", "c"}, "up": {"c"}}, "a,c,b"},
		{url.Values{"order": {"a", "b", "c"}, "down": {"a"}}, "b,a,c"},
		{url.Values{"order": {"a", "b", "c"}, "up": {"a"}}, "a,b,c"},
		{url.Values{"order": {"a", "b", "c"}, "down": {"c"}}, "a,b,c"},
	} {
		request := httptest.NewRequest("POST", "/app/acme/apps/order", strings.NewReader(test.form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := strings.Join(formOrder(request), ","); got != test.want {
			t.Errorf("%v = %s, want %s", test.form, got, test.want)
		}
	}
}
