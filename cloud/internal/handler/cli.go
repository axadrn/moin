package handler

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/axadrn/moin/cloud/internal/githubrepo"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
)

// cliScript installs moin on a Mac or a Linux machine. %[1]s is the site,
// %[2]s the name, %[3]s the repo on GitHub, %[4]s the release keys as
// PEM bodies. The binary comes from the newest release there, like every
// download of moin, so GitHub counts it. It installs only a binary whose
// checksum is in SHA256SUMS, signed by a release key, as `moin update`
// does on a server.
const cliScript = `#!/bin/sh
# Installs %[2]s. Usage: curl -fsSL %[1]s/cli.sh | sh
set -eu
url="https://github.com/%[3]s/releases/latest/download"
case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) echo "%[2]s runs on Linux and macOS." >&2; exit 1 ;;
esac
case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac
file="%[2]s-$os-$arch"

# OpenSSL checks the signature. Ed25519 needs 1.1.1 or newer; the openssl
# of macOS is LibreSSL, which cannot, so Homebrew's counts too.
openssl=
for candidate in openssl /opt/homebrew/opt/openssl@3/bin/openssl /usr/local/opt/openssl@3/bin/openssl; do
	case "$("$candidate" version 2>/dev/null || true)" in
		"OpenSSL 1.1.1"* | "OpenSSL "[3-9].*) openssl=$candidate; break ;;
	esac
done
if [ -z "$openssl" ]; then
	found=$(openssl version 2>/dev/null || true)
	echo "%[2]s checks the signature of what it installs, with OpenSSL 1.1.1 or newer. This machine has ${found:-none}." >&2
	echo "Install it, then run this again: brew install openssl, apt install openssl, apk add openssl or dnf install openssl." >&2
	exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url/$file" -o "$tmp/$file"
curl -fsSL "$url/SHA256SUMS" -o "$tmp/SHA256SUMS"
curl -fsSL "$url/SHA256SUMS.sig" -o "$tmp/SHA256SUMS.sig"

signed=
for key in %[4]s; do
	{ echo "-----BEGIN PUBLIC KEY-----"; echo "$key"; echo "-----END PUBLIC KEY-----"; } > "$tmp/key.pem"
	if "$openssl" pkeyutl -verify -pubin -inkey "$tmp/key.pem" -rawin -in "$tmp/SHA256SUMS" -sigfile "$tmp/SHA256SUMS.sig" > /dev/null 2>&1; then
		signed=yes
		break
	fi
done
if [ -z "$signed" ]; then
	echo "The release's checksums carry no signature of %[2]s's release key. Nothing was installed." >&2
	exit 1
fi
want=$(awk -v file="$file" '$2 == file { print $1 }' "$tmp/SHA256SUMS")
got=$("$openssl" dgst -sha256 -r "$tmp/$file" | cut -d ' ' -f 1)
if [ -z "$want" ] || [ "$want" != "$got" ]; then
	echo "$file is not the one the release signed. Nothing was installed." >&2
	exit 1
fi

dir=/usr/local/bin
if [ ! -w "$dir" ]; then
	dir="$HOME/.local/bin"
	mkdir -p "$dir"
fi
mv "$tmp/$file" "$dir/%[2]s.new"
chmod 755 "$dir/%[2]s.new"
mv "$dir/%[2]s.new" "$dir/%[2]s"
echo "Installed $dir/%[2]s, signed by its release key."
case ":$PATH:" in
	*":$dir:"*) ;;
	*) echo "Add $dir to your PATH." ;;
esac
echo "Next, in the folder of your app: %[2]s deploy <server>"
`

// CLIScript answers the script that installs moin.
func CLIScript(appURL string) http.HandlerFunc {
	appURL = strings.TrimRight(appURL, "/")
	script := fmt.Sprintf(cliScript, appURL, build.Name, githubrepo.Repo, strings.Join(releaseKeysPEM(), " "))
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(script))
	}
}

// releaseKeysPEM are the release keys as openssl reads them: the base64
// between the lines of a PEM public key.
func releaseKeysPEM() []string {
	var keys []string
	for _, encoded := range daemon.ReleaseKeys {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			panic("a release key is not an Ed25519 public key: " + encoded)
		}
		der, err := x509.MarshalPKIXPublicKey(ed25519.PublicKey(raw))
		if err != nil {
			panic(err)
		}
		keys = append(keys, base64.StdEncoding.EncodeToString(der))
	}
	return keys
}
