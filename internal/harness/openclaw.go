package harness

import (
	"os"
	"path/filepath"
)

// OpenClaw detects and configures the OpenClaw agent framework.
type OpenClaw struct{}

func (o OpenClaw) Name() string { return "openclaw" }

func (o OpenClaw) Detect(workDir string) bool {
	// SOUL.md is the most distinctive OpenClaw marker
	if _, err := os.Stat(filepath.Join(workDir, "SOUL.md")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "IDENTITY.md")); err == nil {
		return true
	}
	return false
}

func (o OpenClaw) Layers() []LayerDef {
	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer([]string{
			"SOUL.md", "AGENTS.md", "USER.md", "IDENTITY.md",
			"TOOLS.md", "HEARTBEAT.md", "BOOTSTRAP.md", "MEMORY.md",
			"memory/**",
			"skills/**",
			"canvas/**",
		}),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (o OpenClaw) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(o.Name(), workDir), nil
}

func (o OpenClaw) Ignore() []string {
	return append(CommonIgnorePatterns, CommonCredentialFiles...)
}

func (o OpenClaw) SecretPatterns() []string  { return CommonSecretPatterns }
func (o OpenClaw) DefaultHooks() map[string]string { return nil }

func (o OpenClaw) ExternalPaths(_ string) []ExternalPathDef {
	// OpenClaw stores global state in ~/.openclaw/
	source := ExpandHome("~/.openclaw")
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return nil
	}
	return []ExternalPathDef{{
		Source:        "~/.openclaw",
		ArchivePrefix: "__external__/openclaw/",
	}}
}
