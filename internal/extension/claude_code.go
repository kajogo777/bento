package extension

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudeCode detects the Claude Code agent framework.
type ClaudeCode struct{}

func (c ClaudeCode) Name() string { return "claude-code" }

func (c ClaudeCode) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".claude")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "CLAUDE.md")); err == nil {
		return true
	}
	return false
}

func (c ClaudeCode) Contribute(workDir string) Contribution {
	agentPatterns := []string{"CLAUDE.md", ".claude/**"}

	// Add external session path if it exists
	if extPath := claudeProjectPath(workDir); extPath != "" {
		agentPatterns = append(agentPatterns, extPath+"/")
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
	}
}

// claudeProjectPath returns the absolute path to Claude Code's project-specific
// session directory, or empty string if it doesn't exist.
func claudeProjectPath(workDir string) string {
	absDir, err := filepath.Abs(workDir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}
	hash := strings.ReplaceAll(absDir, string(filepath.Separator), "-")
	source := ExpandHome("~/.claude/projects/" + hash)
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return ""
	}
	return source
}
