package tui

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// openBrowser opens url in the default browser without waiting for it.
// Over SSH or without a display there is none; then you open it by hand.
func openBrowser(url string) error {
	if os.Getenv("SSH_CONNECTION") != "" {
		return errors.New("over ssh")
	}
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
	if err := command.Start(); err != nil {
		return err
	}
	go command.Wait()
	return nil
}
