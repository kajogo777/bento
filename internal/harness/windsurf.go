package harness

import (
	"os"
	"path/filepath"
)

// Windsurf detects and configures the Windsurf agent framework.
type Windsurf struct{}

func (w Windsurf) Name() string { return "windsurf" }

func (w Windsurf) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".windsurf")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func (w Windsurf) Layers() []LayerDef {
	// Order matters: agent and deps before project (first-match-wins in scanner).
	return []LayerDef{
		{
			Name:      "agent",
			Patterns:  []string{".windsurf/rules/**", ".windsurf/**"},
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

func (w Windsurf) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := &SessionConfig{
		Agent:  w.Name(),
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

func (w Windsurf) Ignore() []string {
	return commonIgnorePatterns()
}

func (w Windsurf) SecretPatterns() []string {
	return commonSecretPatterns()
}

func (w Windsurf) DefaultHooks() map[string]string {
	return nil
}
