package harness

// allAgentHarnesses returns the list of agent-specific harnesses to probe.
// Fallback is not included since it always matches.
func allAgentHarnesses() []Harness {
	return []Harness{
		ClaudeCode{},
		Codex{},
		OpenCode{},
		OpenClaw{},
		Cursor{},
	}
}

// Detect probes the workspace directory and returns matching harnesses.
// If multiple agents are detected, returns a CompositeHarness that merges
// their layers. If none are detected, returns the Fallback harness.
func Detect(workDir string) Harness {
	var matched []Harness
	for _, h := range allAgentHarnesses() {
		if h.Detect(workDir) {
			matched = append(matched, h)
		}
	}

	switch len(matched) {
	case 0:
		return Fallback{}
	case 1:
		return matched[0]
	default:
		return NewCompositeHarness(matched)
	}
}

// DetectSingle returns the harness with the given name.
// When explicitly requested by the user, the harness is returned regardless
// of whether its markers are present (force mode).
func DetectSingle(workDir, name string) Harness {
	for _, h := range allAgentHarnesses() {
		if h.Name() == name {
			return h
		}
	}
	return Fallback{}
}

// ResolveHarness returns the appropriate harness based on config.
// If harnessName is empty, "auto", or "default" (backward compat), auto-detect.
// Otherwise, detect the specific named harness.
func ResolveHarness(workDir, harnessName string) Harness {
	switch harnessName {
	case "", "auto", "default":
		return Detect(workDir)
	default:
		return DetectSingle(workDir, harnessName)
	}
}
