// Package reveal shows a file in the OS file manager.
package reveal

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// Command is how goos shows path: selected in its folder where the OS can, else the folder.
func Command(goos, path string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{"-R", path}
	case "windows":
		return "explorer", []string{"/select," + path}
	default:
		return "xdg-open", []string{filepath.Dir(path)}
	}
}

// File opens the file manager at path without waiting for it.
func File(path string) error {
	name, args := Command(runtime.GOOS, path)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
