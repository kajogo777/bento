package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Codex detects the Codex agent framework.
type Codex struct{}

func (c Codex) Name() string { return "codex" }

func (c Codex) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".codex")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func (c Codex) Contribute(workDir string) Contribution {
	agentPatterns := []string{".codex/**"}

	// Add external session rollout directories matching this workspace
	for _, dir := range codexSessionDirs(workDir) {
		agentPatterns = append(agentPatterns, dir+"/")
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

// codexSessionDirs finds rollout directories containing sessions for this workspace.
func codexSessionDirs(workDir string) []string {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = ExpandHome("~/.codex")
	}
	if info, err := os.Stat(codexHome); err != nil || !info.IsDir() {
		return nil
	}

	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(absWork); err == nil {
		absWork = resolved
	}

	sessionsDir := filepath.Join(codexHome, "sessions")
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
