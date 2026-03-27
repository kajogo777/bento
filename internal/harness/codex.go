package harness

import (
	"os"
	"path/filepath"
)

// Codex detects and configures the Codex agent framework.
type Codex struct{}

func (c Codex) Name() string { return "codex" }

func (c Codex) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".codex")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "AGENTS.md")); err == nil {
		return true
	}
	return false
}

func (c Codex) Layers() []LayerDef {
	return []LayerDef{
		{
			Name:      "agent",
			Patterns:  []string{"AGENTS.md", ".codex/**"},
			MediaType: "application/vnd.bento.layer.agent.v1",
			Frequency: ChangesOften,
		},
		{
			Name:      "project",
			Patterns:  commonSourcePatterns(),
			MediaType: "application/vnd.bento.layer.project.v1",
			Frequency: ChangesOften,
		},
		{
			Name:      "deps",
			Patterns:  []string{"node_modules/**", ".venv/**", "vendor/**"},
			MediaType: "application/vnd.bento.layer.deps.v1",
			Frequency: ChangesRarely,
		},
	}
}

func (c Codex) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := &SessionConfig{
		Agent:  c.Name(),
		Status: "paused",
	}

	if out, err := execGit(workDir, "rev-parse", "HEAD"); err == nil {
		cfg.GitSha = out
	}

	if out, err := execGit(workDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		cfg.GitBranch = out
	}

	return cfg, nil
}

func (c Codex) Ignore() []string {
	return commonIgnorePatterns()
}

func (c Codex) SecretPatterns() []string {
	return commonSecretPatterns()
}

func (c Codex) DefaultHooks() map[string]string {
	return map[string]string{
		"post_restore": "test -f .codex/setup.sh && sh .codex/setup.sh || true",
	}
}
