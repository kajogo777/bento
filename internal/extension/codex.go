package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Codex detects the Codex agent framework.
type Codex struct{}

func (c Codex) Name() string                                       { return "codex" }
func (c Codex) NormalizePath(_ string) func(path string) string     { return nil }
func (c Codex) ResolvePath(_ string) func(path string) string       { return nil }

func (c Codex) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".codex")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func (c Codex) Contribute(workDir string) Contribution {
	agentPatterns := []string{".codex/**"}

	// Add external session rollout directories matching this workspace.
	for _, dir := range codexSessionDirs(workDir) {
		agentPatterns = append(agentPatterns, dir+"/")
	}

	// Include the global state database (SQLite) which contains thread metadata,
	// agent memories, and job history. The DB is global (all workspaces) because
	// Codex stores everything in a single SQLite file with no per-workspace
	// isolation at the DB level.
	if f := codexLatestStateDB(); f != "" {
		agentPatterns = append(agentPatterns, f)
	}

	// Include the memories directory if it exists.
	if memDir := codexMemoriesDir(); memDir != "" {
		agentPatterns = append(agentPatterns, memDir+"/")
	}

	// Global guidance and config files. Codex supports AGENTS.md for
	// personal instructions and config in YAML or JSON format. We include
	// all known variants so every Codex version is covered.
	home := codexHome()
	for _, name := range []string{"AGENTS.md", "config.yaml", "config.json"} {
		p := filepath.Join(home, name)
		if fileExists(p) {
			agentPatterns = append(agentPatterns, p)
		}
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
		Hooks: map[string]string{
			"post_restore": "test -f .codex/setup.sh && sh .codex/setup.sh || true",
		},
	}
}

// codexHome returns the Codex home directory.
func codexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	return ExpandHome("~/.codex")
}

// codexLatestStateDB returns the path to the highest-versioned Codex SQLite
// state database, or empty string if none exists.
//
// Codex uses versioned database names like state_5.sqlite. We parse the
// integer version from each filename and pick the highest, rather than
// sorting lexicographically (which would incorrectly rank state_9 above
// state_10).
func codexLatestStateDB() string {
	home := codexHome()
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return ""
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		return ""
	}

	bestVersion := -1
	bestFile := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Match state_N.sqlite (the main thread/session database).
		if !strings.HasPrefix(name, "state_") || !strings.HasSuffix(name, ".sqlite") {
			continue
		}
		// Extract the version number between "state_" and ".sqlite".
		verStr := strings.TrimSuffix(strings.TrimPrefix(name, "state_"), ".sqlite")
		ver, err := strconv.Atoi(verStr)
		if err != nil {
			continue
		}
		if ver > bestVersion {
			bestVersion = ver
			bestFile = filepath.Join(home, name)
		}
	}

	return bestFile
}

// codexMemoriesDir returns the path to the Codex memories directory if it exists.
func codexMemoriesDir() string {
	dir := filepath.Join(codexHome(), "memories")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// codexSessionDirs finds rollout directories containing sessions for this workspace.
// Codex stores JSONL rollout files under ~/.codex/sessions/. Each rollout's first
// line contains a JSON object with a "cwd" field. We match that against the
// current workspace to include only relevant session data.
func codexSessionDirs(workDir string) []string {
	home := codexHome()
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return nil
	}

	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(absWork); err == nil {
		absWork = resolved
	}

	sessionsDir := filepath.Join(home, "sessions")
	if info, err := os.Stat(sessionsDir); err != nil || !info.IsDir() {
		return nil
	}

	seen := make(map[string]bool)
	_ = filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		idx := strings.IndexByte(string(data), '\n')
		var firstLine string
		if idx >= 0 {
			firstLine = string(data[:idx])
		} else {
			firstLine = string(data)
		}
		var meta struct {
			CWD string `json:"cwd"`
		}
		if json.Unmarshal([]byte(firstLine), &meta) == nil && meta.CWD != "" {
			cwdResolved := meta.CWD
			if r, err := filepath.EvalSymlinks(meta.CWD); err == nil {
				cwdResolved = r
			}
			if cwdResolved == absWork || strings.HasPrefix(cwdResolved, absWork+string(filepath.Separator)) {
				dir := filepath.Dir(path)
				if !seen[dir] {
					seen[dir] = true
				}
			}
		}
		return nil
	})

	var dirs []string
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	return dirs
}
