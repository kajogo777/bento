package harness

import (
	"os"
	"path/filepath"
)

// Cursor detects and configures the Cursor agent framework.
type Cursor struct{}

func (c Cursor) Name() string { return "cursor" }

func (c Cursor) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".cursor")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func (c Cursor) Layers() []LayerDef {
	// Order matters: agent and deps before project (first-match-wins in scanner).
	return []LayerDef{
		{
			Name:      "agent",
			Patterns:  []string{".cursor/rules/**", ".cursor/mcp.json", ".cursorrules"},
			MediaType: "application/vnd.bento.layer.agent.v1.tar+gzip",
			Frequency: ChangesOften,
		},
		{
			Name:      "deps",
			Patterns:  []string{"node_modules/**", ".venv/**", "vendor/**"},
			MediaType: "application/vnd.bento.layer.deps.v1.tar+gzip",
			Frequency: ChangesRarely,
		},
		{
			Name:      "project",
			Patterns:  commonSourcePatterns(),
			MediaType: "application/vnd.bento.layer.project.v1.tar+gzip",
			Frequency: ChangesOften,
		},
	}
}

func (c Cursor) SessionConfig(workDir string) (*SessionConfig, error) {
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

func (c Cursor) Ignore() []string {
	return commonIgnorePatterns()
}

func (c Cursor) SecretPatterns() []string {
	return commonSecretPatterns()
}

func (c Cursor) DefaultHooks() map[string]string {
	return nil
}
