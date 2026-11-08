package link

import "testing"

const uuid = "0f8e2c4a-1b3d-4e5f-8a9b-0c1d2e3f4a5b"

func TestParse(t *testing.T) {
	for _, ref := range []string{
		"0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"0F8E2C4A1B3D4E5F8A9B0C1D2E3F4A5B",
		uuid,
	} {
		got, ok := Parse(ref)
		if !ok || got != uuid {
			t.Errorf("Parse(%q) = %q, %v, want %q", ref, got, ok, uuid)
		}
	}
	for _, ref := range []string{
		"",
		"my-app",
		"web-1",
		"my-app-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5",
		"0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5g",
	} {
		if got, ok := Parse(ref); ok {
			t.Errorf("Parse(%q) = %q, want no ID", ref, got)
		}
	}
}

func TestID(t *testing.T) {
	if got := ID(uuid); got != "0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b" {
		t.Errorf("ID = %q", got)
	}
}
