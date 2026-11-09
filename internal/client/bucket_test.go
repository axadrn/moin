package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A bucket that cannot be reached is named as the bucket, not the hub,
// and its signed link stays out of the message.
func TestABucketThatIsGoneIsTheBucket(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	link := gone.URL + "/copy?X-Amz-Signature=secret"
	gone.Close()
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"upload": "u1", "url": link})
	}))
	defer hub.Close()
	api, err := New(Config{URL: hub.URL, Token: "pat_x"})
	if err != nil {
		t.Fatal(err)
	}
	_, deployErr := api.Deploy(context.Background(), "shop", []byte("source"))
	for name, err := range map[string]error{"upload": deployErr, "download": api.FetchCopy(context.Background(), link, nil)} {
		var urlErr *url.Error
		if err == nil || !strings.HasPrefix(err.Error(), "cannot reach your bucket: ") || errors.As(err, &urlErr) || strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
