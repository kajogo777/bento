package extension

import (
	"os"
	"path/filepath"
	"runtime"
)

// OpenCode detects the OpenCode agent framework.
type OpenCode struct{}

func (o OpenCode) Name() string                                       { return "opencode" }
// openCodeStoragePlaceholder is the stable placeholder used in archive paths
// to replace the user-specific storage directory.
const openCodeStoragePlaceholder = "/~/.local/share/opencode/storage"

func (o OpenCode) NormalizePath(_ string) func(path string) string {
	storageDir := openCodeStorageDir()
	if storageDir == "" {
		return nil
	}
	return PrefixReplacer(PortablePath(storageDir), openCodeStoragePlaceholder)
}

func (o OpenCode) ResolvePath(_ string) func(path string) string {
	storageDir := openCodeStorageDir()
	if storageDir == "" {
		return nil
	}
	return PrefixReplacer(openCodeStoragePlaceholder, PortablePath(storageDir))
}

func (o OpenCode) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".opencode")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "opencode.json")); err == nil {
		return true
	}
	// Also detect if sessions exist for this workspace in the global storage.
	if openCodeProjectHash(workDir) != "" {
		return true
	}
	return false
}

func (o OpenCode) Contribute(workDir string) Contribution {
	agentPatterns := []string{".opencode/**", "opencode.json"}

	// Include the global SQLite database (v1.4+) which stores all sessions,
	// messages, and parts. Also include WAL/SHM files for consistency.
	if dbPath := openCodeDBPath(); dbPath != "" {
		agentPatterns = append(agentPatterns, dbPath)
		agentPatterns = append(agentPatterns, dbPath+"-wal")
		agentPatterns = append(agentPatterns, dbPath+"-shm")
	}

	// File-based storage (used by the Go-based OpenCode and some TS versions).
	// Sessions, messages, and parts are stored as individual JSON files under
	// ~/.local/share/opencode/storage/.
	if storageDir := openCodeStorageDir(); storageDir != "" {
		agentPatterns = append(agentPatterns, storageDir+"/")
	}

	// User-global config files under ~/.config/opencode/.
	configDir := openCodeConfigDir()
	if configDir != "" {
		// Config files.
		for _, name := range []string{"opencode.json", "tui.json"} {
			full := filepath.Join(configDir, name)
			if fileExists(full) {
				agentPatterns = append(agentPatterns, full)
			}
		}

		// User-level directories: commands, modes, plugins, skills, tools, themes, agents.
		for _, dir := range []string{"commands", "modes", "plugins", "skills", "tools", "themes", "agents"} {
			full := filepath.Join(configDir, dir)
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				agentPatterns = append(agentPatterns, full+"/")
			}
		}
	}

	// Dotfile fallback (~/.opencode/) — same directories as XDG config.
	dotDir := ExpandHome("~/.opencode")
	if info, err := os.Stat(dotDir); err == nil && info.IsDir() {
		for _, dir := range []string{"commands", "modes", "plugins", "skills", "tools", "themes", "agents"} {
			full := filepath.Join(dotDir, dir)
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				agentPatterns = append(agentPatterns, full+"/")
			}
		}
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

// openCodeStorageDir returns the path to the file-based storage directory
// if it exists. OpenCode stores sessions, messages, parts, and project
// metadata as individual JSON files under storage/.
func openCodeStorageDir() string {
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
