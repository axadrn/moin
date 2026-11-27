package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/axadrn/moin/internal/build"
)

// cliScript installs moin on a Mac or a Linux machine. %[1]s is the site,
// %[2]s the name. The binary comes from /download, which serves the
// downloads directory of the image.
const cliScript = `#!/bin/sh
# Installs %[2]s. Usage: curl -fsSL %[1]s/cli.sh | sh
set -eu
url="%[1]s"
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
dir=/usr/local/bin
if [ ! -w "$dir" ]; then
	dir="$HOME/.local/bin"
	mkdir -p "$dir"
fi
curl -fsSL "$url/download/%[2]s-$os-$arch" -o "$dir/%[2]s.new"
chmod 755 "$dir/%[2]s.new"
mv "$dir/%[2]s.new" "$dir/%[2]s"
echo "Installed $dir/%[2]s."
case ":$PATH:" in
	*":$dir:"*) ;;
	*) echo "Add $dir to your PATH." ;;
esac
echo "Next, in the folder of your app: %[2]s deploy <server>"
`

// CLIScript answers the script that installs moin.
func CLIScript(appURL string) http.HandlerFunc {
	appURL = strings.TrimRight(appURL, "/")
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprintf(w, cliScript, appURL, build.Name)
	}
}
