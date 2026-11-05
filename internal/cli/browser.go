package cli

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// openBrowser opens url in the default browser without waiting for it. It
// fails where there is none to open, such as over SSH; then the user opens
// the page by hand. Tests replace it.
var openBrowser = func(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return errors.New("no display")
		}
		command = exec.Command("xdg-open", url)
	}
	if os.Getenv("SSH_CONNECTION") != "" && runtime.GOOS != "windows" {
		return errors.New("over ssh")
	}
	err := command.Start()
	if err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
