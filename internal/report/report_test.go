package report

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSend(t *testing.T) {
	var got Report
	var auth, path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	sent := Report{
		Server: "prod", Version: "v1.0.0", At: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), Every: time.Minute,
		Apps: []App{{Name: "shop", Release: 3, Running: true, Domains: []string{"shop.example.com"},
			Databases: []Database{{Name: "main", Kind: "postgres", CheckOK: true}}}},
	}
	if err := Send(context.Background(), server.URL+"/", "secret", sent); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer secret" || path != Path {
		t.Errorf("auth = %q, path = %q", auth, path)
	}
	if got.Server != "prod" || got.Every != time.Minute || len(got.Apps) != 1 || got.Apps[0].Databases[0].Kind != "postgres" {
		t.Errorf("got %+v", got)
	}
}

func TestSendNotOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown token", http.StatusUnauthorized)
	}))
	defer server.Close()
	err := Send(context.Background(), server.URL, "wrong", Report{Server: "prod"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401: unknown token") {
		t.Errorf("err = %v", err)
	}
}
