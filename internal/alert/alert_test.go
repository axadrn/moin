package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// receiver is a fake destination: it keeps the path and body of the last post.
func receiver(t *testing.T, status int) (*httptest.Server, *string, *map[string]any) {
	t.Helper()
	var path string
	body := map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &body); err != nil {
			t.Errorf("body is not JSON: %s", data)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "nope")
	}))
	t.Cleanup(server.Close)
	return server, &path, &body
}

var down = Message{Server: "prod", App: "shop", Title: "shop.example.com is down", Text: "No answer within 10s.\n"}

func TestSendWebhook(t *testing.T) {
	server, _, body := receiver(t, http.StatusOK)
	if err := Send(context.Background(), Destination{Kind: "webhook", URL: server.URL}, down); err != nil {
		t.Fatal(err)
	}
	got := *body
	if got["server"] != "prod" || got["app"] != "shop" || got["title"] != down.Title || got["ok"] != false {
		t.Errorf("webhook body = %v", got)
	}
}

func TestSendSlackAndDiscord(t *testing.T) {
	for kind, field := range map[string]string{"slack": "text", "discord": "content"} {
		server, _, body := receiver(t, http.StatusNoContent)
		if err := Send(context.Background(), Destination{Kind: kind, URL: server.URL}, down); err != nil {
			t.Fatal(kind, err)
		}
		text, _ := (*body)[field].(string)
		if !strings.Contains(text, "🔴") || !strings.Contains(text, "shop.example.com is down") || !strings.Contains(text, "prod · shop") {
			t.Errorf("%s text = %q", kind, text)
		}
	}
}

func TestSendTelegram(t *testing.T) {
	server, path, body := receiver(t, http.StatusOK)
	old := telegramAPI
	telegramAPI = server.URL
	t.Cleanup(func() { telegramAPI = old })
	up := Message{Server: "prod", Title: "prod reports <again>", OK: true}
	if err := Send(context.Background(), Destination{Kind: "telegram", Token: "123:abc", ChatID: "-42"}, up); err != nil {
		t.Fatal(err)
	}
	if *path != "/bot123:abc/sendMessage" {
		t.Errorf("path = %q", *path)
	}
	if (*body)["chat_id"] != "-42" || (*body)["text"] != "<b>🟢 prod reports &lt;again&gt;</b>\nprod" {
		t.Errorf("telegram body = %v", *body)
	}
}

// A failed send says the status and the answer, never the secret URL.
func TestSendFailureHidesSecret(t *testing.T) {
	server, _, _ := receiver(t, http.StatusForbidden)
	address := server.URL + "/hook/secret-token"
	err := Send(context.Background(), Destination{Kind: "webhook", URL: address}, down)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403: nope") {
		t.Fatalf("err = %v", err)
	}
	server.Close()
	err = Send(context.Background(), Destination{Kind: "webhook", URL: address}, down)
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Errorf("err = %v, want no secret in it", err)
	}
}

func TestSendRefusesBadDestination(t *testing.T) {
	for _, d := range []Destination{
		{Kind: "mail"}, {Kind: "telegram"}, {Kind: "webhook", URL: "ftp://example.com"},
		{Kind: "slack", URL: "http://hooks.example.com/x"}, {Kind: "webhook", URL: "http://10.0.0.5/hook"}, {Kind: "discord"},
	} {
		if err := Send(context.Background(), d, down); err == nil {
			t.Errorf("Send(%v) = nil, want an error", d)
		}
	}
}

func TestCheck(t *testing.T) {
	for _, d := range []Destination{
		{Kind: "telegram", Token: "123:abc", ChatID: "-42"},
		{Kind: "webhook", URL: "https://example.com/hook"},
		{Kind: "webhook", URL: "http://127.0.0.1:9000/hook"},
		{Kind: "slack", URL: "http://localhost/hook"},
	} {
		if err := Check(d); err != nil {
			t.Errorf("Check(%v) = %v", d, err)
		}
	}
}
