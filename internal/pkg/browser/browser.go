// Package browser opens URLs in the user's default web browser.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Open opens url in the user's default browser. It returns as soon as the
// launcher is started (best effort); it does not wait for the browser to load.
func Open(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	if err := exec.Command(cmd, args...).Start(); err != nil { //nolint:gosec // cmd is from a fixed set
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
