package harness

// allAgentHarnesses returns the list of agent-specific harnesses to probe.
// Fallback is not included since it always matches.
func allAgentHarnesses() []Harness {
	return []Harness{
		ClaudeCode{},
		Codex{},
		Aider{},
		Cursor{},
		Windsurf{},
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

// DetectSingle probes for a specific harness by name.
// Returns Fallback if the named harness is not found or doesn't match.
func DetectSingle(workDir, name string) Harness {
	for _, h := range allAgentHarnesses() {
		if h.Name() == name && h.Detect(workDir) {
			return h
		}
	}
	return Fallback{}
}
