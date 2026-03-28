//go:build !windows

package registry

import "os"

// createDirLink creates a symlink at linkPath pointing to target.
// On Unix, this is a standard symlink with a relative path.
func createDirLink(target, linkPath string) error {
	// Use a relative path so the store is relocatable.
	return os.Symlink("../blobs", linkPath)
}
