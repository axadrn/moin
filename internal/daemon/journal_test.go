package daemon

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// journal catches what the daemon writes to its log during the test.
func journal(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &out
}

// A request of the hub the rules refuse shows in the log: what was asked
// and why the daemon did not do it, without the command's arguments.
func TestJournalShowsARefusedRequest(t *testing.T) {
	out := journal(t)
	rulesFile := filepath.Join(t.TempDir(), "rules")
	if err := os.WriteFile(rulesFile, []byte("exec: off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &runner{rulesFile: rulesFile, pending: map[string]protocol.DeployDone{}}
	message, _ := protocol.NewMessage(protocol.TypeExecStart, protocol.ExecStart{App: "my-app", Service: "web", Argv: []string{"psql", "postgres://u:s3cret@db/x"}})
	message.RequestID = "r1"
	r.handle(t.Context(), message)

	log := out.String()
	for _, want := range []string{`msg="hub asked" type=exec.start request=r1 app=my-app service=web command=psql`, `msg="not done" request=r1 why="refused by the rules of this server`, "exec: off"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log has no %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "s3cret") {
		t.Errorf("the log shows an argument of the command:\n%s", log)
	}
}

// No request writes a value, a secret, a password, SQL or a token to the
// log: only what it names.
func TestJournalShowsNoValue(t *testing.T) {
	const secret = "s3cret-value"
	requests := []struct {
		kind protocol.MessageType
		data any
		want string
	}{
		{protocol.TypeEnvSet, protocol.EnvSet{App: "my-app", Set: map[string]string{"DB_PASSWORD": secret}, Remove: []string{"OLD"}}, "app=my-app set=[DB_PASSWORD] remove=[OLD]"},
		{protocol.TypeSecretSet, protocol.SecretSet{Name: "tunnel", Value: secret}, "name=tunnel"},
		{protocol.TypeBucketSet, protocol.BucketSet{Bucket: protocol.Bucket{Bucket: "backups", AccessKey: secret}, SecretKey: secret}, "type=bucket.set"},
		{protocol.TypeDBQuery, protocol.DBQuery{DBRequest: protocol.DBRequest{App: "my-app"}, SQL: "UPDATE users SET password = '" + secret + "'", Params: []any{secret}, Write: true}, "app=my-app write=true"},
		{protocol.TypeDeployStart, protocol.DeployStart{
			DeployID: "d1", App: "my-app", Number: 12,
			SourceURL:           "https://api.github.com/repos/acme/blog/tarball/abc123?token=" + secret,
			SourceAuthorization: "Bearer " + secret,
			Domains:             &protocol.Domains{Protect: "me:" + secret},
		}, "app=my-app deploy=d1 number=12 source=api.github.com/repos/acme/blog/tarball/abc123"},
	}
	for _, request := range requests {
		out := journal(t)
		message, err := protocol.NewMessage(request.kind, request.data)
		if err != nil {
			t.Fatal(err)
		}
		logAsked(message)
		logAnswer(protocol.TypeError, "r1", protocol.Error{Message: "no"})
		if log := out.String(); !strings.Contains(log, request.want) || strings.Contains(log, secret) {
			t.Errorf("%s: want %q and no value, got:\n%s", request.kind, request.want, log)
		}
	}
}
