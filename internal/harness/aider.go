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
	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer([]string{".aider*", ".aider.tags.cache.v3/**"}),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (a Aider) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(a.Name(), workDir), nil
}

func (a Aider) Ignore() []string         { return CommonIgnorePatterns }
func (a Aider) SecretPatterns() []string  { return CommonSecretPatterns }
func (a Aider) DefaultHooks() map[string]string { return nil }
