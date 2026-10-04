package webui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser asks the desktop to open url. It is a few lines of platform
// dispatch; github.com/pkg/browser does the same but has no tagged releases.
func OpenBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}

	// Reap the launcher in the background so it does not linger as a zombie.
	go func() { _ = command.Wait() }()

	return nil
}
