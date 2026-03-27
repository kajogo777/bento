package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// NormalizePath converts backslashes to forward slashes.
func NormalizePath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// NativePathSep converts forward slashes to the native OS path separator.
func NativePathSep(p string) string {
	return filepath.FromSlash(p)
}

// IsExecutable checks if the file extension suggests an executable script.
func IsExecutable(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".sh", ".bash", ".py", ".rb", ".pl":
		return true
	}
	return false
}

// DefaultFileMode returns the appropriate file mode. Directories and executable
// files get 0755; all other files get 0644.
func DefaultFileMode(name string) os.FileMode {
	if strings.HasSuffix(name, "/") || IsExecutable(name) {
		return 0755
	}
	return 0644
}
