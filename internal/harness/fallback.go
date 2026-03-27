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
	return BaseSessionConfig(f.Name(), workDir), nil
}

func (f Fallback) Ignore() []string         { return CommonIgnorePatterns }
func (f Fallback) SecretPatterns() []string  { return CommonSecretPatterns }
func (f Fallback) DefaultHooks() map[string]string { return nil }
