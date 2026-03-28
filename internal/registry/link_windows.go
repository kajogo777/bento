//go:build windows

package registry

import (
	"os/exec"
	"path/filepath"
)

// createDirLink creates a directory junction at linkPath pointing to target.
// Junctions work without admin privileges on Windows and are transparent
// to applications — they see a normal directory.
func createDirLink(target, linkPath string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	// mklink /J creates a directory junction (no admin required).
	return exec.Command("cmd", "/C", "mklink", "/J", linkPath, absTarget).Run()
}
