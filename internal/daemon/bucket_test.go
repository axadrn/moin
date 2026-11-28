package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// lockBucket is a bucket that refuses what its fields say, with an
// AccessDenied as S3 refuses a locked object.
type lockBucket struct {
	refuseDelete, refusePut bool
	has                     bool
}

func (l *lockBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	refused := r.Method == http.MethodDelete && l.refuseDelete || r.Method == http.MethodPut && l.refusePut
	switch {
	case refused:
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)
	case r.Method == http.MethodHead && !l.has:
		w.WriteHeader(http.StatusNotFound)
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	}
}

// The check says locked when the bucket refuses to delete the fresh
// object, or to overwrite the one an earlier check left. It fails only
// when nothing can be written at all.
func TestCheckBucket(t *testing.T) {
	cases := []struct {
		name   string
		bucket lockBucket
		locked bool
		fails  bool
	}{
		{"deletes", lockBucket{}, false, false},
		{"refuses the delete", lockBucket{refuseDelete: true}, true, false},
		{"refuses to overwrite an earlier check", lockBucket{refusePut: true, has: true}, true, false},
		{"refuses every write", lockBucket{refusePut: true}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(&c.bucket)
			defer server.Close()
			config := Config{Name: "prod", Bucket: &Bucket{Bucket: "b", Endpoint: server.URL, AccessKey: "k", SecretKey: "s"}}
			locked, err := checkBucket(context.Background(), config)
			if locked != c.locked || (err != nil) != c.fails {
				t.Fatalf("locked %v, err %v", locked, err)
			}
		})
	}
}

// An unlocked bucket is a soft note in `moin`, a locked one none.
func TestBucketNeeds(t *testing.T) {
	needs := bucketNeeds(Config{Name: "prod", Bucket: &Bucket{}})
	if len(needs) != 1 || !needs[0].Soft || needs[0].Command != "moin bucket prod" {
		t.Fatalf("not locked: %+v", needs)
	}
	if needs := bucketNeeds(Config{Name: "prod", Bucket: &Bucket{Locked: true}}); len(needs) != 0 {
		t.Fatalf("locked: %+v", needs)
	}
	if needs := bucketNeeds(Config{Name: "prod"}); len(needs) != 0 {
		t.Fatalf("no bucket: %+v", needs)
	}
}
