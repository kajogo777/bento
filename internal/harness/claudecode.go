package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeCode detects and configures the Claude Code agent framework.
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

func (c ClaudeCode) Layers(workDir string) []LayerDef {
	agentPatterns := []string{"CLAUDE.md", ".claude/**"}

	// Add external session path if it exists
	if extPath := claudeProjectPath(workDir); extPath != "" {
		agentPatterns = append(agentPatterns, extPath+"/")
	}

	return []LayerDef{
		DepsLayer(append(CommonDepsPatterns, ".tool-versions")),
		AgentLayer(agentPatterns),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (c ClaudeCode) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := BaseSessionConfig(c.Name(), workDir)
	if out, err := exec.Command("claude", "--version").Output(); err == nil {
		cfg.AgentVersion = strings.TrimSpace(string(out))
	}
	return cfg, nil
}

func (c ClaudeCode) Ignore() []string {
	patterns := append(CommonIgnorePatterns, CommonCredentialFiles...)
	return append(patterns, ".claude/credentials", ".claude/oauth_tokens")
}

func (c ClaudeCode) SecretPatterns() []string { return CommonSecretPatterns }
func (c ClaudeCode) DefaultHooks() map[string]string { return nil }

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
