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

func (c ClaudeCode) Layers() []LayerDef {
	// Order: agent -> deps -> project (first-match-wins in scanner).
	return []LayerDef{
		AgentLayer([]string{"CLAUDE.md", ".claude/**"}),
		DepsLayer(append(CommonDepsPatterns, ".tool-versions")),
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
	return append(CommonIgnorePatterns, ".claude/credentials", ".claude/oauth_tokens")
}

func (c ClaudeCode) SecretPatterns() []string {
	return CommonSecretPatterns
}

func (c ClaudeCode) DefaultHooks() map[string]string {
	return nil
}
