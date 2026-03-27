package harness

import (
	"os"
	"path/filepath"
)

// Codex detects and configures the Codex agent framework.
type Codex struct{}

func (c Codex) Name() string { return "codex" }

func (c Codex) Detect(workDir string) bool {
	// Only detect on .codex/ dir. AGENTS.md alone is ambiguous (OpenCode also uses it).
	if info, err := os.Stat(filepath.Join(workDir, ".codex")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func (c Codex) Layers() []LayerDef {
	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer([]string{"AGENTS.md", ".codex/**"}),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (c Codex) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(c.Name(), workDir), nil
}

func (c Codex) Ignore() []string {
	return append(CommonIgnorePatterns, CommonCredentialFiles...)
}
func (c Codex) SecretPatterns() []string  { return CommonSecretPatterns }
func (c Codex) DefaultHooks() map[string]string {
	return map[string]string{
		"post_restore": "test -f .codex/setup.sh && sh .codex/setup.sh || true",
	}
}

func (c Codex) ExternalPaths(_ string) []ExternalPathDef {
	source := ExpandHome("~/.codex")
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return nil
	}
	return []ExternalPathDef{{
		Source:        "~/.codex",
		ArchivePrefix: "__external__/codex/",
	}}
}
