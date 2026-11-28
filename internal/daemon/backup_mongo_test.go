package daemon

import (
	"errors"
	"testing"
)

// TestMongoError: an error keeps its kind and loses what quotes the app's
// data.
func TestMongoError(t *testing.T) {
	for _, test := range []struct{ stderr, want string }{
		{"2026-11-28T14:50:29.680+0000\tFailed: stream or file does not appear to be a mongodump archive\n", "mongorestore: stream or file does not appear to be a mongodump archive"},
		{"2026-11-28T14:50:29.813+0000\tFailed: corruption found in archive; ParserConsumer.BodyBSON() ( corruption found in archive; read bson ( unexpected EOF ) )\n", "mongorestore: corruption found in archive"},
		{"2026-11-28T14:50:29.813+0000\tFailed: shop.users: error restoring from archive on stdin: E11000 duplicate key error collection: shop.users index: email_1 dup key: { email: \"ada@example.com\" }\n", "mongorestore: shop.users"},
		{"Failed: can't create session: failed to connect: connection() error occurred during connection handshake: auth error: sasl conversation error: unable to authenticate using mechanism \"SCRAM-SHA-256\": (AuthenticationFailed) Authentication failed.\n", "MongoDB did not let moin in: moin logs in with the MONGO_INITDB_ROOT_USERNAME and MONGO_INITDB_ROOT_PASSWORD of the service"},
		{"sh: 1: mongorestore: not found\n", "mongorestore: exit status 127"},
	} {
		if got := mongoError("mongorestore", test.stderr, errors.New("exit status 127")).Error(); got != test.want {
			t.Errorf("mongoError(%q) = %q, want %q", test.stderr, got, test.want)
		}
	}
}
