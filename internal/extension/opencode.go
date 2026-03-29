package extension

import (
	"os"
	"path/filepath"
	"strings"
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

	// Add external session/project/snapshot dirs scoped by git root commit hash
	for _, dir := range openCodeExternalDirs(workDir) {
		agentPatterns = append(agentPatterns, dir+"/")
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
	}
}

func openCodeExternalDirs(workDir string) []string {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		dataDir = ExpandHome("~/.local/share")
	}
	base := filepath.Join(dataDir, "opencode")
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		return nil
	}

	hash := openCodeProjectHash(workDir)
	if hash == "" {
		return nil
	}

	var dirs []string
	for _, sub := range []string{
		filepath.Join("storage", "session", hash),
		filepath.Join("snapshot", hash),
	} {
		dir := filepath.Join(base, sub)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func openCodeProjectHash(workDir string) string {
	out, err := execGit(workDir, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}
