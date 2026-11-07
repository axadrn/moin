package protocol

import (
	"fmt"
	"strings"
)

// DownloadPath is where the control plane serves the binaries, as
// <name>-<os>-<arch>: the install script and daemon updates take them from
// there.
const DownloadPath = "/download/"

// DaemonURL is the address of the control plane a daemon should dial.
type DaemonURL struct {
	URL string `json:"url"`
}

// DaemonUpdate is the binary the daemon should run: the control plane's
// version and the SHA-256 of the binary it serves for the daemon's OS and
// CPU. The daemon downloads it from its own control plane URL.
type DaemonUpdate struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// UpdateCommand updates a daemon by hand on the server, as root: for a
// daemon too old to update itself, or one whose rules keep it from doing so.
func UpdateCommand(baseURL, name, arch string) string {
	binary := "/usr/local/bin/" + name
	return fmt.Sprintf("curl -fsSL %s%s%s-linux-%s -o %s.new && chmod 755 %s.new && mv %s.new %s && systemctl restart %s-daemon",
		strings.TrimRight(baseURL, "/"), DownloadPath, name, arch, binary, binary, binary, binary, name)
}

// DaemonUnit is the systemd unit of the daemon.
func DaemonUnit(name string) string {
	return "[Unit]\n" +
		"Description=" + name + " daemon\n" +
		"After=network-online.target docker.service\n" +
		"Wants=network-online.target docker.service\n" +
		"\n[Service]\n" +
		"ExecStart=/usr/local/bin/" + name + " daemon\n" +
		"Environment=" + strings.ToUpper(name) + "_STATE_DIR=/var/lib/" + name + "\n" +
		"Restart=always\n" +
		"RestartSec=2\n" +
		"\n[Install]\n" +
		"WantedBy=multi-user.target\n"
}

// UninstallCommand removes the daemon from a server, run on it as root: its
// service, binary and state. The apps keep running with their data; add
// --apps to remove them too.
func UninstallCommand(name string) string {
	return name + " daemon uninstall"
}
