package copies

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

// The copy a hacked server can't delete, against a real S3. Run with a
// local SeaweedFS:
//
//	docker run -d -p 127.0.0.1:19100:8333 chrislusf/seaweedfs server -s3
//	MOIN_TEST_S3=http://127.0.0.1:19100 go test ./cloud/internal/copies/

// outbox keeps the mails instead of sending them.
type outbox struct{ sent []mailer.Message }

func (o *outbox) Send(_ context.Context, m mailer.Message) error {
	o.sent = append(o.sent, m)
	return nil
}

// copiesTest is moin cloud with one paid workspace and one server.
type copiesTest struct {
	t      *testing.T
	ctx    context.Context
	db     *sql.DB
	store  *workspace.Store
	copies *Copies
	mails  *outbox
	space  workspace.Workspace
	token  string
}

func newCopiesTest(t *testing.T) *copiesTest {
	t.Helper()
	endpoint := os.Getenv("MOIN_TEST_S3")
	if endpoint == "" {
		t.Skip("set MOIN_TEST_S3 to an S3 endpoint")
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	test := &copiesTest{t: t, ctx: context.Background(), db: database, store: workspace.NewStore(database), mails: &outbox{}}
	// One bucket for every test: each workspace has its own folder.
	bucket := NewBucket(endpoint, "copies-test", "test", "test")
	_, err = bucket.client.CreateBucket(test.ctx, &s3.CreateBucketInput{Bucket: aws.String("copies-test")})
	if err != nil && code(err) != "BucketAlreadyOwnedByYou" && code(err) != "BucketAlreadyExists" {
		t.Fatal(err)
	}
	test.copies = New(test.store, bucket, test.mails, "https://cloud.example.com")
	if test.space, err = test.store.SignIn("ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if test.token, err = test.store.ConnectServer(test.space.ID, "prod"); err != nil {
		t.Fatal(err)
	}
	return test
}

// pay makes the workspace paid, or ended.
func (test *copiesTest) pay(status string) {
	test.t.Helper()
	if err := test.store.SetBilling(test.space.ID, workspace.Billing{Status: status, Customer: "cus_1", Subscription: "sub_1"}); err != nil {
		test.t.Fatal(err)
	}
}

// put sends body to an address as the daemon does: its exact size, and
// only where nothing lies yet.
func put(t *testing.T, address string, body []byte) int {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPut, address, bytes.NewReader(body))
	request.Header.Set("If-None-Match", "*")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(response.Body)
	t.Logf("PUT %d bytes: HTTP %d %s", len(body), response.StatusCode, answer)
	return response.StatusCode
}

func TestCopyUploadAndDownload(t *testing.T) {
	test := newCopiesTest(t)
	body := []byte("age-encryption.org/v1 and then the copy")
	request := Request{App: "shop", Database: "/data/app.db", MadeAt: time.Now().UTC().Truncate(time.Second), Size: int64(len(body))}

	if _, err := test.copies.Start(test.ctx, test.token, request); !errors.Is(err, ErrNotPaid) {
		t.Fatalf("a trial: %v", err)
	}
	if _, err := test.copies.Start(test.ctx, "wrong", request); !errors.Is(err, ErrUnknownToken) {
		t.Fatalf("a wrong token: %v", err)
	}
	test.pay("paid")
	for _, bad := range []Request{{App: "Shop!", Database: "x", MadeAt: request.MadeAt, Size: 1}, {App: "shop", MadeAt: request.MadeAt, Size: 1}, {App: "shop", Database: "x", Size: 1}, {App: "shop", Database: "x", MadeAt: request.MadeAt}, {App: "shop", Database: "x", MadeAt: time.Now().Add(2 * time.Hour), Size: 1}} {
		if _, err := test.copies.Start(test.ctx, test.token, bad); !errors.Is(err, ErrNotACopy) {
			t.Errorf("%+v: %v", bad, err)
		}
	}

	upload, err := test.copies.Start(test.ctx, test.token, request)
	if err != nil {
		t.Fatal(err)
	}
	// The address is signed with the size and If-None-Match.
	parsed, _ := url.Parse(upload.URL)
	if signed := parsed.Query().Get("X-Amz-SignedHeaders"); !strings.Contains(signed, "content-length") || !strings.Contains(signed, "if-none-match") {
		t.Errorf("signed headers %q", signed)
	}
	if parsed.Query().Get("X-Amz-Expires") != "900" {
		t.Errorf("expires %q", parsed.Query().Get("X-Amz-Expires"))
	}
	if err := test.copies.Done(test.ctx, test.token, upload.ID); !errors.Is(err, ErrNotUploaded) {
		t.Fatalf("done before the upload: %v", err)
	}
	// R2 should refuse more bytes than signed; SeaweedFS without keys
	// checks no signature. Done keeps the size in the bucket either way.
	if status := put(t, upload.URL, append(body, "more"...)); status/100 == 2 {
		t.Logf("more bytes than signed went in: HTTP %d", status)
		if _, err := test.copies.bucket.client.DeleteObject(test.ctx, &s3.DeleteObjectInput{Bucket: aws.String("copies-test"), Key: aws.String(test.space.ID + "/" + upload.ID + ".age")}); err != nil {
			t.Fatal(err)
		}
	}
	if status := put(t, upload.URL, body); status != http.StatusOK {
		t.Fatalf("upload: HTTP %d", status)
	}
	if status := put(t, upload.URL, body); status != http.StatusPreconditionFailed {
		t.Errorf("a second upload to the same address: HTTP %d", status)
	}

	other, _ := test.store.ConnectServer(test.space.ID, "web-1")
	if err := test.copies.Done(test.ctx, other, upload.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("done by another server: %v", err)
	}
	if err := test.copies.Done(test.ctx, test.token, upload.ID); err != nil {
		t.Fatal(err)
	}
	list, err := test.copies.List(test.space.ID, "prod", "shop")
	if err != nil || len(list) != 1 || list[0].ID != upload.ID || list[0].Size != int64(len(body)) || !list[0].MadeAt.Equal(request.MadeAt) {
		t.Fatalf("list = %+v, %v", list, err)
	}

	// One copy an hour per database.
	if _, err := test.copies.Start(test.ctx, test.token, request); !errors.Is(err, ErrTooOften) {
		t.Errorf("a second copy at once: %v", err)
	}
	// A full storage refuses, and mails once a day.
	big := Request{App: "shop", Database: "postgres-db", MadeAt: request.MadeAt, Size: Storage}
	for range 2 {
		if _, err := test.copies.Start(test.ctx, test.token, big); !errors.Is(err, ErrFull) {
			t.Errorf("too big: %v", err)
		}
	}
	if len(test.mails.sent) != 1 || !strings.Contains(test.mails.sent[0].Subject, "full") {
		t.Errorf("mails %+v", test.mails.sent)
	}

	address, err := test.copies.DownloadURL(test.ctx, test.space.ID, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !bytes.Equal(got, body) {
		t.Errorf("downloaded %q", got)
	}
	if _, err := test.copies.DownloadURL(test.ctx, "another-workspace", upload.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a copy of another workspace: %v", err)
	}
}

// Old copies go, never the newest of a database; an upload that never
// ended goes after a day; every copy goes 30 days after the end.
func TestPrune(t *testing.T) {
	test := newCopiesTest(t)
	test.pay("paid")
	now := time.Now()
	made := func(database string, at time.Time, done bool) workspace.Copy {
		copy, err := test.store.NewCopy(workspace.Copy{WorkspaceID: test.space.ID, Server: "prod", App: "shop", Database: database, MadeAt: at, Size: 10})
		if err != nil {
			t.Fatal(err)
		}
		if done {
			if err := test.store.CopyDone(copy.ID, 10); err != nil {
				t.Fatal(err)
			}
		}
		return copy
	}
	older := made("/data/app.db", now.Add(-time.Hour), true)
	newest := made("/data/app.db", now, true)
	stuck := made("/data/app.db", now, false)
	alone := made("postgres-db", now.Add(-time.Hour), true)
	left := func() map[string]bool {
		rows, err := test.db.Query(`SELECT id FROM copies`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		found := map[string]bool{}
		for rows.Next() {
			var id string
			rows.Scan(&id)
			found[id] = true
		}
		return found
	}

	if err := test.copies.Prune(test.ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := left(); len(got) != 4 {
		t.Fatalf("after an hour %v", got)
	}
	if err := test.copies.Prune(test.ctx, now.Add(Keep+time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := left(); got[older.ID] || got[stuck.ID] || !got[newest.ID] || !got[alone.ID] {
		t.Fatalf("after 14 days %v: older %s, stuck %s", got, older.ID, stuck.ID)
	}

	test.pay("ended")
	if err := test.copies.Prune(test.ctx, now.Add(Grace-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := left(); len(got) != 2 {
		t.Fatalf("before the grace ended %v", got)
	}
	if err := test.copies.Prune(test.ctx, now.Add(Grace+time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := left(); len(got) != 0 {
		t.Fatalf("after the grace %v", got)
	}
}

// What only a real R2 bucket with its 14-day lock shows: the signed size
// and If-None-Match hold, and nobody deletes a copy inside the lock. Run
// with the settings of moin cloud's bucket (config.go):
//
//	COPIES_ENDPOINT=… COPIES_BUCKET=… COPIES_ACCESS_KEY=… COPIES_SECRET_KEY=… go test -run R2 ./cloud/internal/copies/
//
// The object it leaves stays 14 days: use a bucket for tests.
func TestR2(t *testing.T) {
	endpoint := os.Getenv("COPIES_ENDPOINT")
	if endpoint == "" {
		t.Skip("set COPIES_ENDPOINT and the rest to an R2 bucket with a lock")
	}
	ctx := context.Background()
	bucket := NewBucket(endpoint, os.Getenv("COPIES_BUCKET"), os.Getenv("COPIES_ACCESS_KEY"), os.Getenv("COPIES_SECRET_KEY"))
	key := "test/" + time.Now().UTC().Format("20060102T150405.000000000") + ".age"
	body := []byte("age-encryption.org/v1 and the copy")
	address, err := bucket.UploadURL(ctx, key, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if status := put(t, address, append(body, "more"...)); status/100 == 2 {
		t.Errorf("more bytes than signed went in: HTTP %d", status)
	}
	request, _ := http.NewRequest(http.MethodPut, address, bytes.NewReader(body))
	if response, err := http.DefaultClient.Do(request); err != nil || response.StatusCode/100 == 2 {
		t.Errorf("an upload without If-None-Match went in: %v %v", err, response.Status)
	}
	if status := put(t, address, body); status != http.StatusOK {
		t.Fatalf("upload: HTTP %d", status)
	}
	// R2's lock answers before If-None-Match does: 409, not 412.
	if status := put(t, address, body); status != http.StatusConflict && status != http.StatusPreconditionFailed {
		t.Errorf("an overwrite: HTTP %d", status)
	}
	if size, err := bucket.Size(ctx, key); err != nil || size != int64(len(body)) {
		t.Errorf("size %d, %v", size, err)
	}
	if err := bucket.Delete(ctx, key); !errors.Is(err, errLocked) {
		t.Errorf("a delete inside the lock: %v", err)
	}
	download, err := bucket.DownloadURL(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(download)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !bytes.Equal(got, body) {
		t.Errorf("downloaded %q", got)
	}
	if _, err := bucket.Size(ctx, "test/nothing.age"); !errors.Is(err, errMissing) {
		t.Errorf("a missing object: %v", err)
	}
}
