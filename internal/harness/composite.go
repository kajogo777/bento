package harness

import "strings"

// CompositeHarness combines multiple detected agent harnesses into one.
// Agent layers from all detected harnesses are merged, deps are deduplicated,
// and a single project catch-all layer covers everything else.
type CompositeHarness struct {
	harnesses []Harness
}

func NewCompositeHarness(harnesses []Harness) *CompositeHarness {
	return &CompositeHarness{harnesses: harnesses}
}

func (c *CompositeHarness) Name() string {
	names := make([]string, len(c.harnesses))
	for i, h := range c.harnesses {
		names[i] = h.Name()
	}
	return strings.Join(names, "+")
}

func (c *CompositeHarness) Detect(workDir string) bool {
	return len(c.harnesses) > 0
}

func (c *CompositeHarness) Layers() []LayerDef {
	// Collect agent layers from all harnesses (each gets its own named layer).
	var layers []LayerDef
	seenDepsPatterns := make(map[string]bool)
	var depsPatterns []string
	var projectLayer LayerDef

	for _, h := range c.harnesses {
		for _, ld := range h.Layers() {
			switch {
			case ld.Name == "agent":
				// Rename to agent-<harness> for clarity
				agentLayer := LayerDef{
					Name:      "agent-" + h.Name(),
					Patterns:  ld.Patterns,
					MediaType: ld.MediaType,
					Frequency: ld.Frequency,
				}
				layers = append(layers, agentLayer)
			case ld.Name == "deps":
				// Merge deps patterns, deduplicating
				for _, p := range ld.Patterns {
					if !seenDepsPatterns[p] {
						seenDepsPatterns[p] = true
						depsPatterns = append(depsPatterns, p)
					}
				}
			case ld.CatchAll:
				// Keep the broadest project layer (first one wins)
				if projectLayer.Name == "" {
					projectLayer = ld
				}
			}
		}
	}

	// Add merged deps layer
	if len(depsPatterns) > 0 {
		layers = append(layers, LayerDef{
			Name:      "deps",
			Patterns:  depsPatterns,
			MediaType: "application/vnd.bento.layer.deps.v1.tar+gzip",
			Frequency: ChangesRarely,
		})
	}

	// Add project catch-all layer last
	if projectLayer.Name != "" {
		layers = append(layers, projectLayer)
	} else {
		// Fallback project layer
		layers = append(layers, LayerDef{
			Name:      "project",
			Patterns:  commonSourcePatterns(),
			MediaType: "application/vnd.bento.layer.project.v1.tar+gzip",
			Frequency: ChangesOften,
			CatchAll:  true,
		})
	}

	return layers
}

func (c *CompositeHarness) SessionConfig(workDir string) (*SessionConfig, error) {
	// Use the first harness's session config as base
	if len(c.harnesses) > 0 {
		cfg, err := c.harnesses[0].SessionConfig(workDir)
		if err == nil {
			cfg.Agent = c.Name()
		}
		return cfg, err
	}
	return &SessionConfig{Agent: c.Name(), Status: "paused"}, nil
}

func (c *CompositeHarness) Ignore() []string {
	seen := make(map[string]bool)
	var patterns []string
	for _, h := range c.harnesses {
		for _, p := range h.Ignore() {
			if !seen[p] {
				seen[p] = true
				patterns = append(patterns, p)
			}
		}
	}
	return patterns
}

func (c *CompositeHarness) SecretPatterns() []string {
	seen := make(map[string]bool)
	var patterns []string
	for _, h := range c.harnesses {
		for _, p := range h.SecretPatterns() {
			if !seen[p] {
				seen[p] = true
				patterns = append(patterns, p)
			}
		}
	}
	return patterns
}

func (c *CompositeHarness) DefaultHooks() map[string]string {
	merged := make(map[string]string)
	for _, h := range c.harnesses {
		for k, v := range h.DefaultHooks() {
			if _, exists := merged[k]; !exists {
				merged[k] = v
			}
		}
	}
	return merged
}
