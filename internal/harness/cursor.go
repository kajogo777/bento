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
	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer([]string{".cursor/rules/**", ".cursor/mcp.json", ".cursorrules"}),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (c Cursor) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(c.Name(), workDir), nil
}

func (c Cursor) Ignore() []string         { return CommonIgnorePatterns }
func (c Cursor) SecretPatterns() []string  { return CommonSecretPatterns }
func (c Cursor) DefaultHooks() map[string]string { return nil }
