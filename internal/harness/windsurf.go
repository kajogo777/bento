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
	return []LayerDef{
		AgentLayer([]string{".windsurf/rules/**", ".windsurf/**"}),
		DepsLayer(CommonDepsPatterns),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (w Windsurf) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := &SessionConfig{Agent: w.Name(), Status: "paused"}
	if out, err := execGit(workDir, "rev-parse", "HEAD"); err == nil {
		cfg.GitSha = out
	}
	if out, err := execGit(workDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		cfg.GitBranch = out
	}
	return cfg, nil
}

func (w Windsurf) Ignore() []string         { return CommonIgnorePatterns }
func (w Windsurf) SecretPatterns() []string  { return CommonSecretPatterns }
func (w Windsurf) DefaultHooks() map[string]string { return nil }
