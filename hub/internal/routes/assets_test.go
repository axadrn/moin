package routes

import "testing"

func TestImmutableAssetPathPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: "js/runtime-1.2.3.deadbeef.js", want: true},
		{path: "/js/runtime-1.2.3.deadbeef.js", want: true},
		{path: "/js/runtime-1.2.3.deadbeef.min.js", want: true},
		{path: "/js/runtime.js", want: false},
		{path: "/css/site.deadbeef.css", want: false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			if got := immutableAssetPathPattern.MatchString(test.path); got != test.want {
				t.Fatalf("immutableAssetPathPattern.MatchString(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestFontAssetPathPattern(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"fonts/geist/geist-variable.woff2":              true,
		"/fonts/bricolage/bricolage-grotesque-bold.ttf": true,
		"/fonts/geist/README.md":                        false,
		"/css/output.css":                               false,
	} {
		if got := fontAssetPathPattern.MatchString(path); got != want {
			t.Errorf("fontAssetPathPattern.MatchString(%q) = %t, want %t", path, got, want)
		}
	}
}
