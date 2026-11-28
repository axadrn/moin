package daemon

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/internal/build"
)

// lockBucket is a bucket that keeps the objects written to it, by their
// path, and refuses what its fields say, with an AccessDenied as S3
// refuses a locked object.
type lockBucket struct {
	locked   bool // refuses to overwrite or delete an object it has
	readOnly bool // the keys can't write
	objects  map[string]bool
}

func (l *lockBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if l.objects == nil {
		l.objects = map[string]bool{}
	}
	has := l.objects[r.URL.Path]
	switch r.Method {
	case http.MethodPut:
		if l.readOnly || l.locked && has {
			refuse(w)
			return
		}
		l.objects[r.URL.Path] = true
	case http.MethodHead:
		if !has {
			w.WriteHeader(http.StatusNotFound)
		}
	case http.MethodDelete:
		if l.locked && has {
			refuse(w)
			return
		}
		delete(l.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

func refuse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)
}

// earlierCheck is the object an earlier check left in the bucket.
var earlierCheck = map[string]bool{"/b/prod/" + build.Name + "-lock-check": true}

// The check says locked when the bucket refuses to delete the fresh
// object, or to overwrite the one an earlier check left. Keys that can't
// write at all fail, also when an earlier check left its object.
func TestCheckBucket(t *testing.T) {
	cases := []struct {
		name   string
		bucket lockBucket
		locked bool
		fails  bool
	}{
		{"deletes", lockBucket{}, false, false},
		{"refuses the delete", lockBucket{locked: true}, true, false},
		{"refuses to overwrite an earlier check", lockBucket{locked: true, objects: earlierCheck}, true, false},
		{"read-only keys and an earlier check", lockBucket{readOnly: true, objects: earlierCheck}, false, true},
		{"refuses every write", lockBucket{readOnly: true}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.bucket.objects = maps.Clone(c.bucket.objects)
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
