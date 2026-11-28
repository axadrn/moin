package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeGitHub(t *testing.T, mails string) *GitHub {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_, _ = w.Write([]byte(`{"access_token":"user-token"}`))
		case "/user/emails":
			if r.Header.Get("Authorization") != "Bearer user-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(mails))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return New("id", "secret", server.URL, server.URL)
}

func TestMail(t *testing.T) {
	g := fakeGitHub(t, `[{"email":"old@example.com","primary":false,"verified":true},{"email":"Ada@Example.com","primary":true,"verified":true}]`)
	mail, err := g.Mail(t.Context(), "code", "https://moin.example.com/auth/github/callback")
	if err != nil || mail != "Ada@Example.com" {
		t.Fatalf("mail = %q, %v", mail, err)
	}
	g = fakeGitHub(t, `[{"email":"ada@example.com","primary":true,"verified":false}]`)
	if _, err := g.Mail(t.Context(), "code", ""); !errors.Is(err, ErrNoMail) {
		t.Errorf("an unverified mail: %v", err)
	}
	if url := g.AuthorizeURL("state-1", "https://moin.example.com/auth/github/callback"); !strings.Contains(url, "state=state-1") || !strings.Contains(url, "client_id=id") {
		t.Errorf("authorize url = %s", url)
	}
}
