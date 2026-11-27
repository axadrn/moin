package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/axadrn/moin/protocol"
)

// The address of a copy is https to a public address, nothing else.
func TestCopyAddresses(t *testing.T) {
	for _, bad := range []string{"http://copies.example.com/x", "ftp://copies.example.com/x", "https://user:pw@copies.example.com/x", "https:///x", "not a url"} {
		if err := checkCopyURL(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
	if err := checkCopyURL("https://account.r2.cloudflarestorage.com/bucket/key?X-Amz-Signature=1"); err != nil {
		t.Error(err)
	}
	for _, private := range []string{"127.0.0.1:443", "10.0.0.7:443", "192.168.1.2:443", "100.101.102.103:443", "169.254.169.254:80", "[::1]:443", "[fd00::1]:443", "0.0.0.0:443"} {
		if err := refusePrivate("tcp", private, nil); err == nil {
			t.Errorf("%s was dialed", private)
		}
	}
	if err := refusePrivate("tcp", "203.0.113.7:443", nil); err != nil {
		t.Error(err)
	}
}

// A copy in your bucket goes to a watcher that takes it, as it is, and
// to none that doesn't. Run with a local SeaweedFS (backup_docker_test.go).
func TestCopyToCloud(t *testing.T) {
	endpoint := os.Getenv("MOIN_TEST_S3")
	if endpoint == "" {
		t.Skip("set MOIN_TEST_S3 to an S3 endpoint")
	}
	ctx := context.Background()
	// The bucket moin cloud hands out addresses to, over https.
	var mu sync.Mutex
	var uploaded []byte
	var signed string
	upload := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		uploaded, _ = io.ReadAll(r.Body)
		signed = fmt.Sprintf("%d %s", r.ContentLength, r.Header.Get("If-None-Match"))
	}))
	defer upload.Close()
	before := copyClient
	copyClient = upload.Client()
	defer func() { copyClient = before }()
	// moin cloud, and a watcher of your own that takes no copies.
	var done, started []string
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer cloud-token" {
			http.Error(w, "unknown token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/copies":
			var start copyStart
			json.NewDecoder(r.Body).Decode(&start)
			started = append(started, fmt.Sprintf("%s %s %d", start.App, start.Database, start.Size))
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(copyUpload{ID: "C1", URL: upload.URL + "/copies/C1.age"})
		case "/api/copies/C1/done":
			done = append(done, "C1")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()
	own := httptest.NewServer(http.NotFoundHandler())
	defer own.Close()

	a := newApps(t.TempDir())
	b := a.backups
	bucket := &Bucket{Endpoint: endpoint, Region: "us-east-1", Bucket: "moin-test", Path: fmt.Sprintf("run-%d", time.Now().UnixNano()), AccessKey: "x", SecretKey: "y"}
	_, _ = s3Client(bucket).CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("moin-test")})
	b.configure(Config{Name: "prod", Bucket: bucket, Watchers: []Watcher{{URL: own.URL, Token: "own-token"}, {URL: cloud.URL, Token: "cloud-token"}}})
	database := protocol.Database{Path: "/data/app.db"}
	id := protocol.NewCopyID(time.Now())
	_, folder, _ := b.where("shop", database)
	body := []byte("age-encryption.org/v1 and the copy")
	if _, err := s3Client(bucket).PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket.Bucket), Key: copyKey(folder, protocol.Copy{ID: id}, database), Body: bytes.NewReader(body)}); err != nil {
		t.Fatal(err)
	}

	b.toCloud(ctx, "shop", database, id)
	mu.Lock()
	defer mu.Unlock()
	if !bytes.Equal(uploaded, body) || signed != fmt.Sprintf("%d *", len(body)) {
		t.Errorf("uploaded %q with %q", uploaded, signed)
	}
	if strings.Join(started, ",") != fmt.Sprintf("shop /data/app.db %d", len(body)) || strings.Join(done, ",") != "C1" {
		t.Errorf("started %v, done %v", started, done)
	}
	if left, _ := os.ReadDir(a.dir + "/.."); len(left) > 0 {
		for _, entry := range left {
			if strings.HasPrefix(entry.Name(), ".copy-") {
				t.Errorf("left %s", entry.Name())
			}
		}
	}
}
