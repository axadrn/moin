package model

import "testing"

func TestUserKeepsPasswordStateAfterHashIsRemoved(t *testing.T) {
	hash := "password-hash"
	user := &User{PasswordHash: &hash}
	user.PasswordSet = user.HasPassword()
	user.PasswordHash = nil

	if !user.HasPassword() {
		t.Fatal("HasPassword() = false after password hash was removed from request context")
	}
}

func TestUserInitial(t *testing.T) {
	cases := []struct{ name, email, want string }{
		{"Axel", "h@example.com", "A"},
		{"", "h@example.com", "H"},
		{"  ölaf", "x@example.com", "Ö"},
		{"", "", "?"},
	}
	for _, c := range cases {
		user := &User{Name: c.name, Email: c.email}
		if got := user.Initial(); got != c.want {
			t.Errorf("Initial(%q, %q) = %q, want %q", c.name, c.email, got, c.want)
		}
	}
}

func TestUserInitials(t *testing.T) {
	for name, want := range map[string]string{"Ada Lovelace": "AL", "ada b. lovelace": "AL", "Ada": "A", "": "U", "Émile Zola": "ÉZ"} {
		if got := (&User{Name: name, Email: "user@example.com"}).Initials(); got != want {
			t.Errorf("Initials(%q) = %q, want %q", name, got, want)
		}
	}
}

// An app, a workspace or a member without a name shows the first letter of
// what it has, or a question mark.
func TestInitial(t *testing.T) {
	for text, want := range map[string]string{"shop": "S", " ölaf": "Ö", "ada@example.com": "A", "": "?", "  ": "?"} {
		if got := Initial(text); got != want {
			t.Errorf("Initial(%q) = %q, want %q", text, got, want)
		}
	}
}
