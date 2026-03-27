package harness

import (
	"os"
	"path/filepath"
)

// Aider detects and configures the Aider agent framework.
type Aider struct{}

func (a Aider) Name() string { return "aider" }

func (a Aider) Detect(workDir string) bool {
	if _, err := os.Stat(filepath.Join(workDir, ".aider.conf.yml")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, ".aider.chat.history.md")); err == nil {
		return true
	}
	return false
}

func (a Aider) Layers() []LayerDef {
	// Order matters: agent and deps before project (first-match-wins in scanner).
	return []LayerDef{
		{
			Name:      "agent",
			Patterns:  []string{".aider*", ".aider.tags.cache.v3/**"},
			MediaType: "application/vnd.bento.layer.agent.v1.tar+gzip",
			Frequency: ChangesOften,
		},
		{
			Name:      "deps",
			Patterns:  []string{".venv/**", "node_modules/**"},
			MediaType: "application/vnd.bento.layer.deps.v1.tar+gzip",
			Frequency: ChangesRarely,
		},
		{
			Name:      "project",
			Patterns:  commonSourcePatterns(),
			MediaType: "application/vnd.bento.layer.project.v1.tar+gzip",
			Frequency: ChangesOften,
			CatchAll:  true,
		},
	}
}

func (a Aider) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := &SessionConfig{
		Agent:  a.Name(),
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

func (a Aider) Ignore() []string {
	return commonIgnorePatterns()
}

func (a Aider) SecretPatterns() []string {
	return commonSecretPatterns()
}

func (a Aider) DefaultHooks() map[string]string {
	return nil
}
