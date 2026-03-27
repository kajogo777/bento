package harness

// Detect probes the workspace directory and returns the first matching harness.
// Harnesses are tried in order of specificity, falling back to the default harness.
func Detect(workDir string) Harness {
	candidates := []Harness{
		ClaudeCode{},
		Codex{},
		Aider{},
		Cursor{},
		Windsurf{},
		Fallback{},
	}

	for _, h := range candidates {
		if h.Detect(workDir) {
			return h
		}
	}

	// Fallback always matches, but satisfy the compiler.
	return Fallback{}
}
