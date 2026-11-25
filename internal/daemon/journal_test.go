package daemon

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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
		{protocol.TypeSecretSet, protocol.SecretSet{Name: "tunnel", Value: secret}, "name=tunnel removed=false"},
		{protocol.TypeSecretSet, protocol.SecretSet{Name: "tunnel"}, "name=tunnel removed=true"},
		{protocol.TypeBucketSet, protocol.BucketSet{Bucket: protocol.Bucket{Endpoint: "https://" + secret + "@s3.example.com", Bucket: "backups", AccessKey: secret}, SecretKey: secret}, "type=bucket.set bucket=s3.example.com/backups"},
		{protocol.TypeBackupConfig, protocol.BackupConfig{
			Bucket:     &protocol.Bucket{Region: "eu-central-1", Bucket: "backups", AccessKey: secret, SecretKey: secret},
			AppBuckets: map[string]*protocol.Bucket{"my-app": {Endpoint: "https://s3.example.com", Bucket: "mine", AccessKey: secret}},
		}, "bucket=s3.eu-central-1.amazonaws.com/backups app_buckets=\"[my-app=s3.example.com/mine]\""},
		{protocol.TypeServerConfig, protocol.ServerConfig{SSH: protocol.SSHTailscale}, "ssh=" + protocol.SSHTailscale + " ssh_on_tailscale=false firewall=false"},
		{protocol.TypeDBQuery, protocol.DBQuery{DBRequest: protocol.DBRequest{App: "my-app"}, SQL: "UPDATE users SET password = '" + secret + "'", Params: []any{secret}, Write: true}, "app=my-app write=true"},
		{protocol.TypeDeployStart, protocol.DeployStart{
			DeployID: "d1", App: "my-app", Number: 12,
			SourceURL:           "https://api.github.com/repos/acme/blog/tarball/abc123?token=" + secret,
			SourceAuthorization: "Bearer " + secret,
			Domains:             &protocol.Domains{Password: "me:" + secret},
		}, "app=my-app deploy=d1 number=12 source=api.github.com/repos/acme/blog/tarball/abc123"},
	}
	for _, request := range requests {
		out := journal(t)
		message, err := protocol.NewMessage(request.kind, request.data)
		if err != nil {
			t.Fatal(err)
		}
		logAsked(message)
		if log := out.String(); !strings.Contains(log, request.want) || strings.Contains(log, secret) {
			t.Errorf("%s: want %q and no value, got:\n%s", request.kind, request.want, log)
		}
	}
}

// Why a request was not done shows in full only for a refusal or a
// sentence of the daemon's own: any other error may carry a value.
func TestJournalShowsWhyOnlyWhenItIsSafe(t *testing.T) {
	const secret = "s3cret-value"
	out := journal(t)
	logAnswer(protocol.TypeError, "r1", protocol.Error{Message: `near "` + secret + `": syntax error`})
	logAnswer(protocol.TypeDeployDone, "", protocol.DeployDone{DeployID: "d1", Error: "web: DATABASE_URL=postgres://u:" + secret + "@db/x: exit status 1"})
	logAnswer(protocol.TypeDeployDone, "", protocol.DeployDone{DeployID: "d2", Error: "service web: volume /srv/" + secret + " is not allowed: " + beyondTheApp + ". Allow it with server-access: on in /etc/moin/rules"})
	logAnswer(protocol.TypeError, "r2", protocol.Error{Message: refused("/etc/moin/rules", "data: off").Error()})
	logAnswer(protocol.TypeServerConfigResult, "r3", protocol.ServerConfigResult{Problems: []string{"SSH: sshd said " + secret}})
	logAnswer(protocol.TypeOK, "r4", nil)
	log := out.String()
	for _, want := range []string{
		`msg="not done" request=r1 why=failed`,
		`msg="not done" deploy=d1 why=failed`,
		`msg="not done" deploy=d2 why="refused: ` + beyondTheApp,
		`msg="not done" request=r2 why="refused by the rules of this server (/etc/moin/rules): data: off"`,
		`msg="not fully done" request=r3 problems=SSH`,
		`msg=done request=r4`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log has no %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, secret) {
		t.Errorf("the log shows a value:\n%s", log)
	}
}

// A long value is clipped, so the hub cannot fill the journal.
func TestJournalClipsValues(t *testing.T) {
	out := journal(t)
	message, _ := protocol.NewMessage(protocol.TypeAppStop, protocol.AppStop{App: strings.Repeat("a", 300)})
	logAsked(message)
	if log := out.String(); strings.Contains(log, strings.Repeat("a", 201)) || !strings.Contains(log, strings.Repeat("a", 200)) {
		t.Errorf("the app is not clipped at 200 bytes:\n%s", log)
	}
	if got := clip(strings.Repeat("a", 199) + "äb"); got != strings.Repeat("a", 199) {
		t.Errorf("clip cut a character: %q", got)
	}
}

// A deploy.done sent again on a new session is not logged again.
func TestJournalLogsADeployOnce(t *testing.T) {
	out := journal(t)
	r := &runner{pending: map[string]protocol.DeployDone{}}
	r.write(protocol.TypeDeployDone, "", protocol.DeployDone{DeployID: "d1", OK: true})
	if out.Len() != 0 {
		t.Errorf("a deploy.done sent again is logged:\n%s", out)
	}
}

// Every field of every request the hub sends carries a mark of its own. Only
// the fields that are safe to show reach the log.
func TestJournalShowsOnlyWhatItMay(t *testing.T) {
	requests := map[protocol.MessageType]any{
		protocol.TypeDeployStart:    &protocol.DeployStart{},
		protocol.TypeLogs:           &protocol.LogsRequest{},
		protocol.TypeAppRemove:      &protocol.AppRemove{},
		protocol.TypeAppStop:        &protocol.AppStop{},
		protocol.TypeDBTables:       &protocol.DBRequest{},
		protocol.TypeDBQuery:        &protocol.DBQuery{},
		protocol.TypeServerConfig:   &protocol.ServerConfig{},
		protocol.TypeBackupConfig:   &protocol.BackupConfig{},
		protocol.TypeEnvSet:         &protocol.EnvSet{},
		protocol.TypeSecretSet:      &protocol.SecretSet{},
		protocol.TypeBucketSet:      &protocol.BucketSet{},
		protocol.TypeBackupLink:     &protocol.BackupLink{},
		protocol.TypeBackupList:     &protocol.BackupRequest{},
		protocol.TypeBackupRestores: &protocol.BackupRequest{},
		protocol.TypeBackupRestore:  &protocol.BackupRestore{},
		protocol.TypeExecStart:      &protocol.ExecStart{},
		protocol.TypeDaemonURL:      &protocol.DaemonURL{},
		protocol.TypeDaemonUpdate:   &protocol.DaemonUpdate{},
	}
	may := map[string]bool{
		"DeployStart.DeployID": true, "DeployStart.App": true, "DeployStart.Name": true, "DeployStart.SourceURL": true, "DeployStart.Image": true, "DeployStart.Catalog": true,
		"LogsRequest.App": true, "AppRemove.App": true, "AppStop.App": true,
		"DBRequest.App": true, "DBRequest.Database": true, "DBQuery.DBRequest.App": true, "DBQuery.DBRequest.Database": true,
		"ServerConfig.SSH":           true,
		"BackupConfig.Bucket.Bucket": true, "BackupConfig.AppBuckets{key}": true, "BackupConfig.AppBuckets{}.Bucket": true,
		"EnvSet.App": true, "EnvSet.Set{key}": true, "EnvSet.Remove[0]": true, "EnvSet.Remove[1]": true,
		"SecretSet.Name":          true,
		"BucketSet.Bucket.Bucket": true,
		"BackupLink.App":          true, "BackupLink.Database": true, "BackupLink.Copy": true, "BackupLink.Name": true,
		"BackupRequest.App": true, "BackupRequest.Database": true,
		"BackupRestore.App": true, "BackupRestore.Database": true, "BackupRestore.Copy": true, "BackupRestore.Undo": true,
		"ExecStart.App": true, "ExecStart.Service": true, "ExecStart.Argv[0]": true,
		"DaemonURL.URL": true, "DaemonUpdate.Version": true,
	}
	marks := map[string]string{}
	shown := 0
	for kind, request := range requests {
		value := reflect.ValueOf(request).Elem()
		fill(value, value.Type().Name(), marks)
		message, err := protocol.NewMessage(kind, request)
		if err != nil {
			t.Fatal(err)
		}
		out := journal(t)
		logAsked(message)
		for _, mark := range regexp.MustCompile(`mk\d+x`).FindAllString(out.String(), -1) {
			shown++
			if !may[marks[mark]] {
				t.Errorf("%s: the log shows %s:\n%s", kind, marks[mark], out)
			}
		}
	}
	if shown < len(may)/2 {
		t.Errorf("only %d marks in the log: the test checks nothing", shown)
	}
}

// fill puts a mark of its own in every string of value, two elements in
// every list and one in every map, and keeps which field each mark is in.
func fill(value reflect.Value, path string, marks map[string]string) {
	mark := func(path string) string {
		m := fmt.Sprintf("mk%dx", len(marks))
		marks[m] = path
		return m
	}
	switch value.Kind() {
	case reflect.String:
		value.SetString(mark(path))
	case reflect.Interface:
		value.Set(reflect.ValueOf(mark(path)))
	case reflect.Pointer:
		value.Set(reflect.New(value.Type().Elem()))
		fill(value.Elem(), path, marks)
	case reflect.Struct:
		for i := range value.NumField() {
			if field := value.Type().Field(i); field.IsExported() {
				fill(value.Field(i), path+"."+field.Name, marks)
			}
		}
	case reflect.Slice:
		list := reflect.MakeSlice(value.Type(), 2, 2)
		for i := range 2 {
			fill(list.Index(i), fmt.Sprintf("%s[%d]", path, i), marks)
		}
		value.Set(list)
	case reflect.Map:
		key := reflect.New(value.Type().Key()).Elem()
		fill(key, path+"{key}", marks)
		element := reflect.New(value.Type().Elem()).Elem()
		fill(element, path+"{}", marks)
		value.Set(reflect.MakeMap(value.Type()))
		value.SetMapIndex(key, element)
	}
}
