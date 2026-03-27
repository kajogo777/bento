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
	cfg := &SessionConfig{
		Agent:  c.Name(),
		Status: "paused",
	}

	if out, err := exec.Command("claude", "--version").Output(); err == nil {
		cfg.AgentVersion = strings.TrimSpace(string(out))
	}

	if out, err := execGit(workDir, "rev-parse", "HEAD"); err == nil {
		cfg.GitSha = out
	}

	if out, err := execGit(workDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		cfg.GitBranch = out
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

// execGit runs a git command in the given directory and returns trimmed output.
func execGit(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
