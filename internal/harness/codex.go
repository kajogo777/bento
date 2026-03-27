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
		AgentLayer([]string{"AGENTS.md", ".codex/**"}),
		DepsLayer(CommonDepsPatterns),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (c Codex) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(c.Name(), workDir), nil
}

func (c Codex) Ignore() []string         { return CommonIgnorePatterns }
func (c Codex) SecretPatterns() []string  { return CommonSecretPatterns }
func (c Codex) DefaultHooks() map[string]string {
	return map[string]string{
		"post_restore": "test -f .codex/setup.sh && sh .codex/setup.sh || true",
	}
}
