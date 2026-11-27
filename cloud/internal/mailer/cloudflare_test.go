package mailer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareSends(t *testing.T) {
	var got cloudflareRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/acc/email/sending/send" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("request %s with %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"permanent_bounces":[]}}`))
	}))
	defer server.Close()
	err := NewCloudflare("acc", "tok", "moin <noreply@example.com>", server.URL).Send(context.Background(), Message{To: "ada@example.com", Subject: "Hi", Text: "text"})
	if err != nil {
		t.Fatal(err)
	}
	if got.From.Address != "noreply@example.com" || got.From.Name != "moin" || got.To != "ada@example.com" || got.Text != "text" {
		t.Fatalf("request = %+v", got)
	}
}

func TestCloudflareFailures(t *testing.T) {
	for name, answer := range map[string]string{
		"sender not verified": `{"success":false,"errors":[{"code":10001,"message":"sender not verified"}]}`,
		"bounced":             `{"success":true,"result":{"permanent_bounces":["ada@example.com"]}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(answer))
		}))
		err := NewCloudflare("acc", "tok", "noreply@example.com", server.URL).Send(context.Background(), Message{To: "ada@example.com"})
		server.Close()
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestNewWithoutAccountLogs(t *testing.T) {
	if _, ok := New("", "", "noreply@example.com", "").(Log); !ok {
		t.Error("without an account mail does not go to the log")
	}
}
