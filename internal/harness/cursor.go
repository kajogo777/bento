package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Cursor detects and configures the Cursor agent framework.
type Cursor struct{}

func (c Cursor) Name() string { return "cursor" }

func (c Cursor) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".cursor")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, ".cursorrules")); err == nil {
		return true
	}
	return false
}

func (c Cursor) Layers() []LayerDef {
	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer([]string{".cursor/rules/**", ".cursor/mcp.json", ".cursorrules"}),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (c Cursor) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(c.Name(), workDir), nil
}

func (c Cursor) Ignore() []string {
	return append(CommonIgnorePatterns, CommonCredentialFiles...)
}
func (c Cursor) SecretPatterns() []string  { return CommonSecretPatterns }
func (c Cursor) DefaultHooks() map[string]string { return nil }

// cursorWorkspaceStoragePath returns the platform-specific Cursor data directory.
func cursorWorkspaceStoragePath() string {
	switch runtime.GOOS {
	case "darwin":
		return ExpandHome("~/Library/Application Support/Cursor/User/workspaceStorage")
	case "linux":
		return ExpandHome("~/.config/Cursor/User/workspaceStorage")
	default: // windows
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "Cursor", "User", "workspaceStorage")
		}
		return ""
	}
}

// cursorFindWorkspaceHash scans Cursor's workspaceStorage to find the hash
// directory whose workspace.json maps to the given workDir.
func cursorFindWorkspaceHash(storagePath, workDir string) string {
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absWork); err == nil {
		absWork = resolved
	}

	entries, err := os.ReadDir(storagePath)
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wsFile := filepath.Join(storagePath, e.Name(), "workspace.json")
		data, err := os.ReadFile(wsFile)
		if err != nil {
			continue
		}
		var ws struct {
			Folder string `json:"folder"`
		}
		if json.Unmarshal(data, &ws) != nil || ws.Folder == "" {
			continue
		}
		// Cursor stores as file:// URI, strip prefix
		folder := ws.Folder
		if len(folder) > 7 && folder[:7] == "file://" {
			folder = folder[7:]
		}
		if resolved, err := filepath.EvalSymlinks(folder); err == nil {
			folder = resolved
		}
		if folder == absWork {
			return e.Name()
		}
	}
	return ""
}

func (c Cursor) ExternalPaths(workDir string) []ExternalPathDef {
	storagePath := cursorWorkspaceStoragePath()
	if storagePath == "" {
		return nil
	}
	if info, err := os.Stat(storagePath); err != nil || !info.IsDir() {
		return nil
	}

	hash := cursorFindWorkspaceHash(storagePath, workDir)
	if hash == "" {
		return nil
	}

	source := filepath.Join(storagePath, hash)
	return []ExternalPathDef{{
		Source:        source,
		ArchivePrefix: "__external__/cursor/",
	}}
}
