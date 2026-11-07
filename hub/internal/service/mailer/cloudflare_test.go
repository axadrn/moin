package mailer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareMailerSends(t *testing.T) {
	var got cloudflareRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/acc/email/sending/send" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("request %s with %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"permanent_bounces":[]}}`))
	}))
	defer server.Close()

	mailer := NewCloudflareMailer("acc", "tok", "moin <noreply@example.com>")
	mailer.api = server.URL
	err := mailer.Send(context.Background(), Message{To: "a@example.com", Subject: "Hi", Text: "text", HTML: "<p>html</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if got.From.Address != "noreply@example.com" || got.From.Name != "moin" || got.To != "a@example.com" || got.HTML != "<p>html</p>" {
		t.Fatalf("request = %+v", got)
	}
}

func TestCloudflareMailerReportsFailures(t *testing.T) {
	for name, answer := range map[string]string{
		"error":  `{"success":false,"errors":[{"code":10001,"message":"sender not verified"}]}`,
		"bounce": `{"success":true,"result":{"permanent_bounces":["a@example.com"]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(answer))
			}))
			defer server.Close()
			mailer := NewCloudflareMailer("acc", "tok", "noreply@example.com")
			mailer.api = server.URL
			err := mailer.Send(context.Background(), Message{To: "a@example.com"})
			if err == nil || !strings.Contains(err.Error(), map[string]string{"error": "sender not verified", "bounce": "bounced"}[name]) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
