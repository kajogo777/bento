package harness

// Fallback is the default harness used when no specific agent framework is detected.
type Fallback struct{}

func (f Fallback) Name() string { return "default" }

func (f Fallback) Detect(_ string) bool { return true }

func (f Fallback) Layers() []LayerDef {
	return []LayerDef{
		{
			Name:      "agent",
			Patterns:  []string{},
			MediaType: "application/vnd.bento.layer.agent.v1",
			Frequency: ChangesOften,
		},
		{
			Name:      "project",
			Patterns:  commonSourcePatterns(),
			MediaType: "application/vnd.bento.layer.project.v1",
			Frequency: ChangesOften,
		},
		{
			Name:      "deps",
			Patterns:  []string{"node_modules/**", ".venv/**", "vendor/**", "__pycache__/**"},
			MediaType: "application/vnd.bento.layer.deps.v1",
			Frequency: ChangesRarely,
		},
	}
}

func (f Fallback) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := &SessionConfig{
		Agent:  f.Name(),
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

func (f Fallback) Ignore() []string {
	return commonIgnorePatterns()
}

func (f Fallback) SecretPatterns() []string {
	return commonSecretPatterns()
}

func (f Fallback) DefaultHooks() map[string]string {
	return nil
}
