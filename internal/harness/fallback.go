package harness

// Fallback is the default harness used when no specific agent framework is detected.
type Fallback struct{}

func (f Fallback) Name() string            { return "default" }
func (f Fallback) Detect(_ string) bool    { return true }

func (f Fallback) Layers() []LayerDef {
	return []LayerDef{
		AgentLayer(nil),
		DepsLayer(append(CommonDepsPatterns, "__pycache__/**")),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (f Fallback) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := &SessionConfig{Agent: f.Name(), Status: "paused"}
	if out, err := execGit(workDir, "rev-parse", "HEAD"); err == nil {
		cfg.GitSha = out
	}
	if out, err := execGit(workDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		cfg.GitBranch = out
	}
	return cfg, nil
}

func (f Fallback) Ignore() []string         { return CommonIgnorePatterns }
func (f Fallback) SecretPatterns() []string  { return CommonSecretPatterns }
func (f Fallback) DefaultHooks() map[string]string { return nil }
