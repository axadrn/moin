package handler

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/build"
)

// cli.sh checks downloads with the keys `moin update` trusts.
func TestCLIScriptHasTheReleaseKeys(t *testing.T) {
	recorder := httptest.NewRecorder()
	CLIScript("https://moin.example.com/")(recorder, httptest.NewRequest("GET", "/cli.sh", nil))
	body, _ := io.ReadAll(recorder.Body)
	script := string(body)
	if strings.Contains(script, "%!") {
		t.Fatalf("the script has a formatting error:\n%s", script)
	}
	if !strings.Contains(script, allowedSigners(build.ReleaseKeys)+"KEYS\n") {
		t.Errorf("the script does not check with the release keys:\n%s", script)
	}
}

// What scripts/sshsig.sh signs in the Deploy workflow, ssh-keygen accepts
// with the allowed signers cli.sh has, and only for the file it signed.
func TestSSHSignatureOfARelease(t *testing.T) {
	for _, tool := range []string{"openssl", "ssh-keygen"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
	}
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sums := filepath.Join(dir, "SHA256SUMS")
	allowed := filepath.Join(dir, "allowed_signers")
	write := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(sums, "1234  moin-linux-amd64\n")
	write(allowed, allowedSigners([]string{base64.StdEncoding.EncodeToString(public)}))

	sign := exec.Command("sh", "../../../scripts/sshsig.sh", sums)
	sign.Stdin = bytes.NewReader(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	signature, err := sign.Output()
	if err != nil {
		t.Fatalf("sshsig.sh: %v", err)
	}
	write(sums+".sshsig", string(signature))

	verify := func() error {
		cmd := exec.Command("ssh-keygen", "-Y", "verify", "-f", allowed, "-I", build.Name+"-release", "-n", build.Name+"-release", "-s", sums+".sshsig")
		cmd.Stdin, _ = os.Open(sums)
		return cmd.Run()
	}
	if err := verify(); err != nil {
		t.Fatalf("ssh-keygen does not accept the signature: %v", err)
	}
	write(sums, "1234  moin-linux-amd64\n5678  moin-linux-arm64\n")
	if verify() == nil {
		t.Error("ssh-keygen accepts the signature for other checksums")
	}
}
