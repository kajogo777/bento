package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpenCode detects and configures the OpenCode agent framework.
type OpenCode struct{}

func (o OpenCode) Name() string { return "opencode" }

func (o OpenCode) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".opencode")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "opencode.json")); err == nil {
		return true
	}
	return false
}

func (o OpenCode) Layers() []LayerDef {
	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer([]string{
			"AGENTS.md",
			".opencode/**",
			"opencode.json",
		}),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (o OpenCode) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := BaseSessionConfig(o.Name(), workDir)
	if out, err := exec.Command("opencode", "--version").Output(); err == nil {
		cfg.AgentVersion = strings.TrimSpace(string(out))
	}
	return cfg, nil
}

func (o OpenCode) Ignore() []string {
	return append(CommonIgnorePatterns, CommonCredentialFiles...)
}

func (o OpenCode) SecretPatterns() []string { return CommonSecretPatterns }
func (o OpenCode) DefaultHooks() map[string]string { return nil }

func (o OpenCode) ExternalPaths(_ string) []ExternalPathDef {
	// OpenCode stores sessions in ~/.local/share/opencode/ (XDG data dir)
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		dataDir = ExpandHome("~/.local/share")
	}
	source := filepath.Join(dataDir, "opencode")
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return nil
	}
	return []ExternalPathDef{{
		Source:        source,
		ArchivePrefix: "__external__/opencode/",
	}}
}
