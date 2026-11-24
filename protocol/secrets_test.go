package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestSecretMessagesRoundTrip: what the hub sends about secrets reads back
// the same on the daemon.
func TestSecretMessagesRoundTrip(t *testing.T) {
	for _, test := range []struct {
		kind MessageType
		data any
		into any
	}{
		{TypeEnvSet, EnvSet{App: "shop", Set: map[string]string{"KEY": "a$b\n"}, Remove: []string{"OLD"}}, &EnvSet{}},
		{TypeSecretSet, SecretSet{Name: RegistrySecret("ghcr.io"), Value: "me:token"}, &SecretSet{}},
		{TypeBucketSet, BucketSet{Bucket: Bucket{ID: "b1", Bucket: "moin", Workspace: "ws-1"}, SecretKey: "s3cret"}, &BucketSet{}},
		{TypeBackupLink, BackupLink{App: "shop", Database: "/data/app.db", Copy: "c1", Name: "shop"}, &BackupLink{}},
		{TypeHello, Hello{ProtocolVersion: Version, Secrets: []string{"bucket/b1", "tunnel"}}, &Hello{}},
	} {
		message, err := NewMessage(test.kind, test.data)
		if err != nil {
			t.Fatal(err)
		}
		if err := message.Decode(test.into); err != nil {
			t.Fatal(err)
		}
		if got := reflect.ValueOf(test.into).Elem().Interface(); !reflect.DeepEqual(got, test.data) {
			t.Errorf("%s: %+v, want %+v", test.kind, got, test.data)
		}
	}
}

// TestNoSecretInDeploysAndConfigs: a deploy, a backup config and a server
// config carry no secret, so a hub cannot keep one by accident.
func TestNoSecretInDeploysAndConfigs(t *testing.T) {
	for _, data := range []any{
		DeployStart{App: "shop", Image: "ghcr.io/me/shop:1"},
		BackupConfig{Bucket: &Bucket{ID: "b1", AccessKey: "key", SecretKey: "s3cret"}},
		ServerConfig{SSH: SSHTailscale, Firewall: true},
	} {
		body, _ := json.Marshal(data)
		for _, word := range []string{"s3cret", "secret_key", "env\"", "registry", "tunnel_token", "tailscale_key"} {
			if strings.Contains(string(body), word) {
				t.Errorf("%T carries %s: %s", data, word, body)
			}
		}
	}
}

func TestSecretNames(t *testing.T) {
	for name, want := range map[string][2]string{
		BucketSecret("b1"):        {"bucket", "b1"},
		RegistrySecret("ghcr.io"): {"registry", "ghcr.io"},
		SecretTailscale:           {"tailscale", ""},
	} {
		if kind, of := SecretOf(name); kind != want[0] || of != want[1] {
			t.Errorf("SecretOf(%q) = %q, %q", name, kind, of)
		}
	}
	if RegistryHost("ghcr.io/me/shop:1") != "ghcr.io" || RegistryHost("me/shop") != "docker.io" || RegistryHost("localhost:5000/shop") != "localhost:5000" {
		t.Error("RegistryHost")
	}
}
