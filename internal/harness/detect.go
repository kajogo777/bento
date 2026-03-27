package harness

// allAgentHarnesses returns the list of agent-specific harnesses to probe.
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

// ResolveAgent returns the appropriate harness based on the agent config value.
// If agentName is empty or "auto", auto-detect. Otherwise, force the named agent.
func ResolveAgent(workDir, agentName string) Harness {
	switch agentName {
	case "", "auto":
		return Detect(workDir)
	default:
		return DetectSingle(workDir, agentName)
	}
}
