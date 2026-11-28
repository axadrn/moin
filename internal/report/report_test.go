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
		Server: "prod", Version: "v1.0.0", EverySeconds: 60,
		Apps: []App{{Name: "shop", Release: 3, Running: true, Domains: []string{"shop.example.com"},
			Databases: []Database{{Name: "main", Kind: "postgres", CheckOK: true, LastCopy: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)}}}},
	}
	if err := Send(context.Background(), server.URL+"/", "secret", sent); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer secret" || path != Path {
		t.Errorf("auth = %q, path = %q", auth, path)
	}
	if got.Server != "prod" || got.EverySeconds != 60 || len(got.Apps) != 1 || got.Apps[0].Databases[0].Kind != "postgres" {
		t.Errorf("got %+v", got)
	}
}

// A report of a daemon before the alerts reads as one without, and a
// report without alerts sends no such field.
func TestAlertsOptional(t *testing.T) {
	var old Report
	if err := json.Unmarshal([]byte(`{"server":"prod","version":"v1.0.0","every_seconds":60,"apps":[]}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Server != "prod" || old.Alerts != nil || old.Problems != nil {
		t.Errorf("old report = %+v", old)
	}
	body, _ := json.Marshal(Report{Server: "prod", Apps: []App{{Name: "shop"}}, Alerts: []Alert{{Kind: "slack", URL: "https://hooks.example.com/x"}}})
	for _, field := range []string{"problems", "stopped", "secret"} {
		if strings.Contains(string(body), field) {
			t.Errorf("a report without %s says %s", field, body)
		}
	}
	body, _ = json.Marshal(Report{Server: "prod"})
	if strings.Contains(string(body), "alerts") {
		t.Errorf("a report without alerts says %s", body)
	}
	body, _ = json.Marshal(Report{Server: "prod", Alerts: []Alert{{Kind: "telegram", Token: "123:test", ChatID: "42"}}})
	var got Report
	if err := json.Unmarshal(body, &got); err != nil || len(got.Alerts) != 1 || got.Alerts[0].ChatID != "42" {
		t.Errorf("got %+v, %v from %s", got, err, body)
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

func TestSendWantsHTTPS(t *testing.T) {
	err := Send(context.Background(), "http://watcher.example.com", "secret", Report{Server: "prod"})
	if err == nil || !strings.Contains(err.Error(), "https://") {
		t.Errorf("err = %v, want https asked for", err)
	}
}

// http without TLS goes only to this machine or into a tailnet, which
// WireGuard encrypts.
func TestPlainHTTP(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "127.0.0.1": true, "::1": true,
		"100.64.0.1": true, "100.127.255.254": true, "fd7a:115c:a1e0::1": true,
		"100.128.0.1": false, "100.63.255.255": false, "10.0.0.5": false, "203.0.113.7": false, "fd7a:115c:a1e1::1": false,
		"": false,
	} {
		if got := PlainHTTP(host); got != want {
			t.Errorf("PlainHTTP(%q) = %v, want %v", host, got, want)
		}
	}
	if err := checkURL("http://100.101.102.103:8080"); err != nil {
		t.Errorf("a tailnet watcher over http: %v", err)
	}
}
