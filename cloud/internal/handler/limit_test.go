package handler

import (
	"testing"
	"time"
)

func TestLimit(t *testing.T) {
	l := newLimit(3, 15*time.Minute)
	now := time.Now()
	for i := range 3 {
		if !l.allow("ada@example.com", now.Add(time.Duration(i)*time.Minute)) {
			t.Fatalf("refused %d", i+1)
		}
	}
	if l.allow("ada@example.com", now.Add(5*time.Minute)) {
		t.Error("allowed a fourth")
	}
	if !l.allow("bob@example.com", now) {
		t.Error("another key counts the same")
	}
	if !l.allow("ada@example.com", now.Add(15*time.Minute)) {
		t.Error("the first one did not run out")
	}
}

func TestLocalPath(t *testing.T) {
	for next, want := range map[string]string{
		"/cli?code=BCDF-GHJK":  "/cli?code=BCDF-GHJK",
		"":                     "/cloud",
		"https://evil.example": "/cloud",
		"//evil.example":       "/cloud",
		`/\evil.example`:       "/cloud",
		"cloud":                "/cloud",
		"javascript:alert(1)":  "/cloud",
		"/cloud#servers":       "/cloud#servers",
	} {
		if got := localPath(next); got != want {
			t.Errorf("localPath(%q) = %q, want %q", next, got, want)
		}
	}
}
