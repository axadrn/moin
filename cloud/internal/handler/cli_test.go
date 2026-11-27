package handler

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/daemon"
)

// cli.sh checks downloads with the keys `moin update` trusts, as openssl
// reads them.
func TestCLIScriptHasTheReleaseKeys(t *testing.T) {
	recorder := httptest.NewRecorder()
	CLIScript("https://moin.example.com/")(recorder, httptest.NewRequest("GET", "/cli.sh", nil))
	body, _ := io.ReadAll(recorder.Body)
	script := string(body)
	if strings.Contains(script, "%!") {
		t.Fatalf("the script has a formatting error:\n%s", script)
	}
	for _, encoded := range daemon.ReleaseKeys {
		raw, _ := base64.StdEncoding.DecodeString(encoded)
		der, _ := x509.MarshalPKIXPublicKey(ed25519.PublicKey(raw))
		pem := base64.StdEncoding.EncodeToString(der)
		if !strings.Contains(script, pem) {
			t.Errorf("the script does not check with the release key %s", encoded)
		}
		parsed, err := x509.ParsePKIXPublicKey(der)
		if err != nil || !ed25519.PublicKey(raw).Equal(parsed) {
			t.Errorf("the PEM of %s is not that key: %v", encoded, err)
		}
	}
}
