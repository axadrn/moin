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
