package client

import (
	"context"
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
	api, err := New(Config{URL: "https://moin.example.com", Token: "pat_x"})
	if err != nil {
		t.Fatal(err)
	}
	err = api.FetchCopy(context.Background(), link, nil)
	var urlErr *url.Error
	if err == nil || !strings.HasPrefix(err.Error(), "cannot reach your bucket: ") || errors.As(err, &urlErr) || strings.Contains(err.Error(), "secret") {
		t.Errorf("download: %v", err)
	}
}
