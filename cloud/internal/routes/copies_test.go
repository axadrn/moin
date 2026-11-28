package routes

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/copies"
	"github.com/axadrn/moin/cloud/internal/counts"
	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/cloud/internal/github"
	"github.com/axadrn/moin/cloud/internal/handler"
	"github.com/axadrn/moin/cloud/internal/watchers"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

// A moin cloud without a bucket takes no copies, and says so.
func TestNoCopies(t *testing.T) {
	server, _, _ := testSite(t)
	postJSON(t, http.DefaultClient, server.URL+"/api/copies", "token", map[string]any{"app": "shop"}, http.StatusServiceUnavailable, nil)
}

// A server sends a copy and the CLI downloads it, as the daemon and the
// CLI do, against a real S3. Run with a local SeaweedFS:
//
//	docker run -d -p 127.0.0.1:19100:8333 chrislusf/seaweedfs server -s3
//	MOIN_TEST_S3=http://127.0.0.1:19100 go test ./cloud/internal/routes/
func TestCopyRoundTrip(t *testing.T) {
	endpoint := os.Getenv("MOIN_TEST_S3")
	if endpoint == "" {
		t.Skip("set MOIN_TEST_S3 to an S3 endpoint")
	}
	client := s3.New(s3.Options{Region: "auto", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""), BaseEndpoint: aws.String(endpoint), UsePathStyle: true})
	_, _ = client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("copies-test")})

	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store := workspace.NewStore(database)
	mails := &outbox{}
	server := httptest.NewUnstartedServer(nil)
	appURL := "http://" + server.Listener.Addr().String()
	cloud := handler.NewCloud(store, watchers.New(store, counts.New(database), mails, appURL, [32]byte{}), mails, github.New("", "", "", ""), appURL, false)
	cloud.Copies = copies.New(store, copies.NewBucket(endpoint, "copies-test", "test", "test"), mails, appURL)
	site, err := New(&config.Config{RenderConfig: config.RenderConfig{AppURL: appURL}}, cloud)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = site
	server.Start()
	t.Cleanup(server.Close)

	space, _ := store.SignIn("ada@example.com")
	session, _ := store.StartSession(space.ID, "cli")
	token, _ := store.ConnectServer(space.ID, "prod")
	body := []byte("age-encryption.org/v1 and the copy")
	start := map[string]any{"app": "shop", "database": "/data/app.db", "made_at": time.Now().UTC(), "size": len(body)}

	postJSON(t, http.DefaultClient, server.URL+"/api/copies", "wrong", start, http.StatusUnauthorized, nil)
	// A trial that is over takes no copies.
	if err := store.StartTrial(space.ID, time.Now().Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	postJSON(t, http.DefaultClient, server.URL+"/api/copies", token, start, http.StatusPaymentRequired, nil)
	// The daemon asks with an empty body whether copies are taken.
	postJSON(t, http.DefaultClient, server.URL+"/api/copies", token, map[string]any{}, http.StatusPaymentRequired, nil)
	if err := store.SetBilling(space.ID, workspace.Billing{Status: "paid"}); err != nil {
		t.Fatal(err)
	}
	postJSON(t, http.DefaultClient, server.URL+"/api/copies", token, map[string]any{"app": "shop"}, http.StatusBadRequest, nil)
	var upload struct{ ID, URL string }
	postJSON(t, http.DefaultClient, server.URL+"/api/copies", token, start, http.StatusCreated, &upload)
	postJSON(t, http.DefaultClient, server.URL+"/api/copies", token, start, http.StatusTooManyRequests, nil)
	postJSON(t, http.DefaultClient, server.URL+"/api/copies/"+upload.ID+"/done", token, nil, http.StatusConflict, nil)

	request, _ := http.NewRequest(http.MethodPut, upload.URL, bytes.NewReader(body))
	request.Header.Set("If-None-Match", "*")
	if response, err := http.DefaultClient.Do(request); err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("upload: %v %v", err, response.Status)
	}
	postJSON(t, http.DefaultClient, server.URL+"/api/copies/"+upload.ID+"/done", token, nil, http.StatusNoContent, nil)

	var list []struct {
		ID, Server, App, Database string
		Size                      int64
	}
	getJSON(t, server.URL+"/api/cloud/copies?server=prod&app=shop", session, &list)
	if len(list) != 1 || list[0].ID != upload.ID || list[0].Server != "prod" || list[0].Size != int64(len(body)) {
		t.Fatalf("copies = %+v", list)
	}
	var download struct{ URL string }
	postJSON(t, http.DefaultClient, server.URL+"/api/cloud/copies/"+upload.ID+"/download", session, nil, http.StatusOK, &download)
	postJSON(t, http.DefaultClient, server.URL+"/api/cloud/copies/nothing/download", session, nil, http.StatusNotFound, nil)
	response, err := http.Get(download.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !bytes.Equal(got, body) {
		t.Errorf("downloaded %q", got)
	}
}
