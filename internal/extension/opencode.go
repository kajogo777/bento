package extension

import (
	"os"
	"path/filepath"
	"runtime"
)

// OpenCode detects the OpenCode agent framework.
type OpenCode struct{}

func (o OpenCode) Name() string { return "opencode" }

func (o OpenCode) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".opencode")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "opencode.json")); err == nil {
		return true
	}
	return false
}

func (o OpenCode) Contribute(workDir string) Contribution {
	agentPatterns := []string{".opencode/**", "opencode.json"}

	// Include the global SQLite database which stores all sessions, messages,
	// and file snapshots. OpenCode uses a single DB for all projects with no
	// per-workspace isolation at the database level.
	if dbPath := openCodeDBPath(); dbPath != "" {
		agentPatterns = append(agentPatterns, dbPath)
	}

	// Legacy file-based storage from older OpenCode versions. Before the
	// SQLite migration, sessions and messages were stored as individual
	// files under ~/.local/share/opencode/storage/.
	if storageDir := openCodeLegacyStorageDir(); storageDir != "" {
		agentPatterns = append(agentPatterns, storageDir+"/")
	}

	// User-level custom commands. OpenCode looks in two locations:
	// XDG config dir (~/.config/opencode/commands/) and the dotfile
	// fallback (~/.opencode/commands/).
	if cmdDir := openCodeUserCommandsDir(); cmdDir != "" {
		agentPatterns = append(agentPatterns, cmdDir+"/")
	}
	if cmdDir := openCodeDotfileCommandsDir(); cmdDir != "" {
		agentPatterns = append(agentPatterns, cmdDir+"/")
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
	}
}

// openCodeDataDir returns the OpenCode data directory.
// Respects XDG_DATA_HOME on Linux/macOS, LOCALAPPDATA on Windows.
func openCodeDataDir() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			return filepath.Join(appData, "opencode")
		}
		return ""
	}
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		dataDir = ExpandHome("~/.local/share")
	}
	return filepath.Join(dataDir, "opencode")
}

// openCodeDBPath returns the path to the OpenCode SQLite database if it exists.
// OpenCode stores all sessions, messages, and file snapshots in a single
// opencode.db file with WAL mode (sessions, messages, files tables).
func openCodeDBPath() string {
	base := openCodeDataDir()
	if base == "" {
		return ""
	}
	dbPath := filepath.Join(base, "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		return ""
	}
	return dbPath
}

// openCodeLegacyStorageDir returns the path to the older file-based storage
// directory if it exists. Before the SQLite migration, OpenCode stored
// sessions and messages as individual files under storage/session/ and
// storage/message/.
func openCodeLegacyStorageDir() string {
	base := openCodeDataDir()
	if base == "" {
		return ""
	}
	dir := filepath.Join(base, "storage")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// openCodeConfigDir returns the XDG config directory for OpenCode.
// Respects XDG_CONFIG_HOME; defaults to ~/.config on Linux/macOS.
func openCodeConfigDir() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			return filepath.Join(appData, "opencode")
		}
		return ""
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		configDir = ExpandHome("~/.config")
	}
	return filepath.Join(configDir, "opencode")
}

// openCodeUserCommandsDir returns the XDG-based user commands directory
// (~/.config/opencode/commands/) if it exists.
func openCodeUserCommandsDir() string {
	base := openCodeConfigDir()
	if base == "" {
		return ""
	}
	dir := filepath.Join(base, "commands")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// openCodeDotfileCommandsDir returns the dotfile-based user commands
// directory (~/.opencode/commands/) if it exists. This is an alternative
// location some OpenCode versions check.
func openCodeDotfileCommandsDir() string {
	dir := ExpandHome("~/.opencode/commands")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}
