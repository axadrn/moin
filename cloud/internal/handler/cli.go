package handler

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"

	"github.com/axadrn/moin/cloud/internal/githubrepo"
	"github.com/axadrn/moin/internal/build"
)

// cliScript installs moin on a Mac or a Linux machine. %[1]s is the site,
// %[2]s the name, %[3]s the repo on GitHub, %[4]s the release keys as
// ssh-keygen's allowed signers. The binary comes from the newest release
// there, like every download of moin, so GitHub counts it. It installs
// only a binary whose checksum is in SHA256SUMS, signed by a release key:
// ssh-keygen checks the signature, as every Mac and Linux has it.
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
if ! command -v ssh-keygen > /dev/null 2>&1; then
	echo "%[2]s checks the signature of what it installs with ssh-keygen, from OpenSSH 8.1 or newer. This machine has none." >&2
	echo "Install it, then run this again: apt install openssh-client, apk add openssh-keygen or dnf install openssh-clients." >&2
	exit 1
fi
sha256="sha256sum"
if ! command -v sha256sum > /dev/null 2>&1; then
	sha256="shasum -a 256"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url/$file" -o "$tmp/$file"
curl -fsSL "$url/SHA256SUMS" -o "$tmp/SHA256SUMS"
curl -fsSL "$url/SHA256SUMS.sshsig" -o "$tmp/SHA256SUMS.sshsig"

cat > "$tmp/allowed_signers" << 'KEYS'
%[4]sKEYS
if ! said=$(ssh-keygen -Y verify -f "$tmp/allowed_signers" -I %[2]s-release -n %[2]s-release -s "$tmp/SHA256SUMS.sshsig" < "$tmp/SHA256SUMS" 2>&1); then
	echo "The release's checksums carry no signature of %[2]s's release key. Nothing was installed." >&2
	echo "ssh-keygen said: $said" >&2
	echo "If ssh-keygen is older than OpenSSH 8.1 (see ssh -V), update it and run this again." >&2
	exit 1
fi
want=$(awk -v file="$file" '$2 == file { print $1 }' "$tmp/SHA256SUMS")
got=$($sha256 "$tmp/$file" | cut -d ' ' -f 1)
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
	script := fmt.Sprintf(cliScript, appURL, build.Name, githubrepo.Repo, allowedSigners(build.ReleaseKeys))
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(script))
	}
}

// allowedSigners are the Ed25519 keys, base64, as ssh-keygen's allowed
// signers: one line per key, the principal and the key as in
// authorized_keys.
func allowedSigners(keys []string) string {
	var lines strings.Builder
	for _, encoded := range keys {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) != 32 {
			panic("a release key is not an Ed25519 public key: " + encoded)
		}
		var blob []byte
		for _, part := range [][]byte{[]byte("ssh-ed25519"), raw} {
			blob = binary.BigEndian.AppendUint32(blob, uint32(len(part)))
			blob = append(blob, part...)
		}
		fmt.Fprintf(&lines, "%s-release ssh-ed25519 %s\n", build.Name, base64.StdEncoding.EncodeToString(blob))
	}
	return lines.String()
}
