package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Cursor detects the Cursor agent framework.
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

func (c Cursor) Contribute(workDir string) Contribution {
	agentPatterns := []string{
		".cursor/rules/**",
		".cursor/mcp.json",
		".cursorrules",
		".cursorignore", // ignore patterns for Cursor indexing
	}

	if wsDir := cursorWorkspaceDir(workDir); wsDir != "" {
		agentPatterns = append(agentPatterns, wsDir+"/")
	}

	// Global MCP server configuration. Some Cursor versions store
	// user-level MCP configs under ~/.cursor/ rather than in the
	// project .cursor/ directory.
	globalMCP := ExpandHome("~/.cursor/mcp.json")
	if fileExists(globalMCP) {
		agentPatterns = append(agentPatterns, globalMCP)
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
	}
}

// cursorWorkspacePlaceholder returns the platform-appropriate placeholder
// for Cursor's workspace storage directory.
func cursorWorkspacePlaceholder() string {
	base := ""
	switch runtime.GOOS {
	case "darwin":
		base = "/~/Library/Application Support/Cursor/User/workspaceStorage"
	case "linux":
		base = "/~/.config/Cursor/User/workspaceStorage"
	default:
		// Windows: APPDATA is not under home, so we use absolute path form.
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return ""
		}
		base = PortablePath(filepath.Join(appData, "Cursor", "User", "workspaceStorage"))
	}
	return base + "/__BENTO_WORKSPACE__"
}

func (c Cursor) NormalizePath(workDir string) func(path string) string {
	wsDir := cursorWorkspaceDir(workDir)
	if wsDir == "" {
		return nil
	}
	placeholder := cursorWorkspacePlaceholder()
	if placeholder == "" {
		return nil
	}
	return PrefixReplacer(PortablePath(wsDir), placeholder)
}

func (c Cursor) ResolvePath(workDir string) func(path string) string {
	storagePath := cursorWorkspaceStoragePath()
	if storagePath == "" {
		return nil
	}
	hash := cursorFindWorkspaceHash(storagePath, workDir)
	if hash == "" {
		return nil
	}
	placeholder := cursorWorkspacePlaceholder()
	if placeholder == "" {
		return nil
	}
	return PrefixReplacer(placeholder, PortablePath(filepath.Join(storagePath, hash)))
}

func cursorWorkspaceDir(workDir string) string {
	storagePath := cursorWorkspaceStoragePath()
	if storagePath == "" {
		return ""
	}
	if info, err := os.Stat(storagePath); err != nil || !info.IsDir() {
		return ""
	}
	hash := cursorFindWorkspaceHash(storagePath, workDir)
	if hash == "" {
		return ""
	}
	return filepath.Join(storagePath, hash)
}

func cursorWorkspaceStoragePath() string {
	switch runtime.GOOS {
	case "darwin":
		return ExpandHome("~/Library/Application Support/Cursor/User/workspaceStorage")
	case "linux":
		return ExpandHome("~/.config/Cursor/User/workspaceStorage")
	default:
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "Cursor", "User", "workspaceStorage")
		}
		return ""
	}
}

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
		data, err := os.ReadFile(filepath.Join(storagePath, e.Name(), "workspace.json"))
		if err != nil {
			continue
		}
		var ws struct {
			Folder string `json:"folder"`
		}
		if json.Unmarshal(data, &ws) != nil || ws.Folder == "" {
			continue
		}
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
