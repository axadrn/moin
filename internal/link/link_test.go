package link

import "testing"

const uuid = "0f8e2c4a-1b3d-4e5f-8a9b-0c1d2e3f4a5b"

func TestTo(t *testing.T) {
	for name, want := range map[string]string{
		"my-app":             "my-app-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"web-1":              "web-1-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"Acme Inc.":          "acme-inc-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"  --Ada's  Team-- ": "ada-s-team-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"":                   "0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"→":                  "0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
	} {
		if got := To(name, uuid); got != want {
			t.Errorf("To(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	for _, ref := range []string{
		"my-app-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		// Only the ID counts: an old or a wrong name finds the same thing.
		"blog-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"a-name-with-many-dashes-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
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
		// One character short, a letter that is not hex, no dash before it.
		"my-app-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5",
		"my-app-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5g",
		"my-app0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b",
		"0f8e2c4a-1b3d-4e5f-8a9b-0c1d2e3f4a5",
	} {
		if got, ok := Parse(ref); ok {
			t.Errorf("Parse(%q) = %q, want no ID", ref, got)
		}
	}
}

func TestToParses(t *testing.T) {
	got, ok := Parse(To("my-app", uuid))
	if !ok || got != uuid {
		t.Fatalf("Parse(To) = %q, %v", got, ok)
	}
	if ID(uuid) != "0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b" {
		t.Fatalf("ID = %q", ID(uuid))
	}
}
