package tui

import (
	"fmt"
	"path/filepath"
	"strings"
)

// textExtensions is the set of file extensions that are considered text-previewable.
var textExtensions = map[string]bool{
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".md": true, ".txt": true, ".go": true, ".py": true,
	".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	".env": true, ".cfg": true, ".ini": true, ".conf": true,
	".xml": true, ".html": true, ".css": true, ".sql": true,
	".log": true, ".csv": true, ".tsv": true,
	".rb": true, ".rs": true, ".java": true, ".kt": true,
	".c": true, ".h": true, ".cpp": true, ".hpp": true,
	".swift": true, ".m": true, ".mm": true,
	".ex": true, ".exs": true, ".erl": true,
	".ml": true, ".mli": true, ".hs": true,
	".lua": true, ".vim": true, ".el": true,
	".r": true, ".R": true, ".jl": true,
	".dockerfile": true, ".tf": true, ".hcl": true,
	".makefile": true, ".cmake": true, ".gradle": true,
	".lock": true, ".sum": true, ".mod": true,
	".gitignore": true, ".gitattributes": true,
	".editorconfig": true, ".prettierrc": true,
	".eslintrc": true, ".stylelintrc": true,
}

// IsTextFile returns true if the file path has a text-previewable extension.
// Also returns true for known extensionless text files.
func IsTextFile(path string) bool {
	base := filepath.Base(path)

	// Known extensionless text files
	switch base {
	case "Makefile", "Dockerfile", "Gemfile", "Rakefile", "Procfile",
		"Vagrantfile", "Brewfile", "Justfile", "Taskfile",
		"CLAUDE.md", "AGENTS.md", "SOUL.md", "IDENTITY.md", "MEMORY.md",
		"LICENSE", "LICENCE", "NOTICE", "AUTHORS", "CONTRIBUTORS",
		"CHANGELOG", "CHANGES", "HISTORY", "NEWS", "TODO", "README":
		return true
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false
	}
	return textExtensions[ext]
}

// FormatSize formats a byte count into a human-readable string.
// Mirrors the existing formatSize in cli/save.go.
func FormatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// TruncateDigest shortens a digest string for display.
// Mirrors the existing truncateDigest in cli/diff.go.
func TruncateDigest(d string) string {
	if len(d) > 19 {
		return d[:19] + "..."
	}
	return d
}

// SplitLines splits data into lines, matching cli/diff.go splitLines.
func SplitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := string(data)
	// Remove trailing newline to avoid a phantom empty line
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
