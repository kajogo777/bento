package harness

import (
	"strings"

	"github.com/kajogo777/bento/internal/manifest"
)

// CompositeHarness combines multiple detected agent harnesses into one.
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

func (c *CompositeHarness) Layers(workDir string) []LayerDef {
	seenDepsPatterns := make(map[string]bool)
	var depsPatterns []string
	var agentLayers []LayerDef
	var projectLayer LayerDef

	for _, h := range c.harnesses {
		for _, ld := range h.Layers(workDir) {
			switch {
			case ld.Name == "agent":
				agentLayers = append(agentLayers, LayerDef{
					Name:      "agent-" + h.Name(),
					Patterns:  ld.Patterns,
					MediaType: ld.MediaType,
				})
			case ld.Name == "deps":
				for _, p := range ld.Patterns {
					if !seenDepsPatterns[p] {
						seenDepsPatterns[p] = true
						depsPatterns = append(depsPatterns, p)
					}
				}
			case ld.CatchAll:
				if projectLayer.Name == "" {
					projectLayer = ld
				}
			}
		}
	}

	var layers []LayerDef
	if len(depsPatterns) > 0 {
		layers = append(layers, LayerDef{
			Name:      "deps",
			Patterns:  depsPatterns,
			MediaType: manifest.MediaTypeDeps,
		})
	}
	layers = append(layers, agentLayers...)
	if projectLayer.Name != "" {
		layers = append(layers, projectLayer)
	} else {
		layers = append(layers, ProjectLayer(CommonSourcePatterns))
	}

	return layers
}

func (c *CompositeHarness) SessionConfig(workDir string) (*SessionConfig, error) {
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
