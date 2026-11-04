package protocol

import "testing"

// TestDetectedBuilt: a Railpack app says its language, every other kind
// itself.
func TestDetectedBuilt(t *testing.T) {
	for _, test := range []struct {
		detected Detected
		want     string
	}{
		{Detected{Kind: "railpack", Builder: "Node 24.22.1, start: npm run start"}, "Node 24.22.1 (Railpack)"},
		{Detected{Kind: "railpack", Builder: "Python 3.13.16"}, "Python 3.13.16 (Railpack)"},
		{Detected{Kind: "railpack"}, "railpack"},
		{Detected{Kind: "go"}, "go"},
		{Detected{Kind: "compose", Builder: "ignored"}, "compose"},
	} {
		if got := test.detected.Built(); got != test.want {
			t.Errorf("Built(%+v) = %q, want %q", test.detected, got, test.want)
		}
	}
}
