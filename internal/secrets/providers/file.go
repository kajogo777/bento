package providers

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// FileProvider resolves secrets by reading a file from disk.
type FileProvider struct{}

// Name returns "file".
func (p *FileProvider) Name() string { return "file" }

// Resolve reads the file at ref.Fields["path"] and returns its contents with
// leading and trailing whitespace trimmed.
func (p *FileProvider) Resolve(ctx context.Context, ref SecretRef) (string, error) {
	path := ref.Fields["path"]
	if path == "" {
		return "", fmt.Errorf("file provider: missing 'path' field")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("file provider: reading %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Available always returns true.
func (p *FileProvider) Available() bool { return true }
