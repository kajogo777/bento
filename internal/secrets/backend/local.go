package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalBackend stores secrets as JSON files on the local filesystem.
// This is the default backend — zero external dependencies.
//
// Storage layout:
//
//	~/.bento/secrets/<workspaceID>/<tag>.json
//
// Each file contains a JSON object mapping placeholder IDs to secret values.
// File permissions are 0600, directory permissions 0700.
type LocalBackend struct {
	// basePath overrides the default secrets directory (for testing).
	basePath string
}

func (b *LocalBackend) Name() string { return "local" }

func (b *LocalBackend) Put(ctx context.Context, key string, secrets map[string]string) (map[string]string, error) {
	path := b.pathForKey(key)

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("creating secrets directory: %w", err)
	}

	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling secrets: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, fmt.Errorf("writing secrets file: %w", err)
	}

	return nil, nil
}

func (b *LocalBackend) Get(ctx context.Context, key string, opts map[string]string) (map[string]string, error) {
	path := b.pathForKey(key)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no secrets found for %q — they may have been stored on another machine", key)
		}
		return nil, fmt.Errorf("reading secrets file: %w", err)
	}

	var secrets map[string]string
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("parsing secrets file: %w", err)
	}

	return secrets, nil
}

func (b *LocalBackend) Delete(ctx context.Context, key string) error {
	path := b.pathForKey(key)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting secrets file: %w", err)
	}

	// Clean up empty parent directory.
	dir := filepath.Dir(path)
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		_ = os.Remove(dir)
	}

	return nil
}

func (b *LocalBackend) Available() bool { return true }

func (b *LocalBackend) Hint(key string, meta map[string]string) (string, string) {
	tag := key
	if idx := strings.LastIndex(key, "/"); idx >= 0 {
		tag = key[idx+1:]
	}

	display := fmt.Sprintf("Secrets stored locally. To share:\n   bento secrets export %s > bundle.json", tag)
	persist := fmt.Sprintf("Secrets were stored locally on the original machine.\n   Ask the sender to export: bento secrets export %s > bundle.json\n   Then import:              bento secrets import < bundle.json", tag)
	return display, persist
}

// pathForKey returns the filesystem path for a given checkpoint key.
func (b *LocalBackend) pathForKey(key string) string {
	base := b.basePath
	if base == "" {
		base = defaultSecretsDir()
	}
	// key is "workspaceID/tag" — split into directory and filename.
	return filepath.Join(base, key+".json")
}

// defaultSecretsDir returns the platform-appropriate secrets directory.
func defaultSecretsDir() string {
	switch runtime.GOOS {
	case "linux":
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "bento", "secrets")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "bento", "secrets")
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "bento", "secrets")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Local", "bento", "secrets")
	default: // darwin
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".bento", "secrets")
	}
}
