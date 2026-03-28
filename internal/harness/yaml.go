package harness

import (
	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/manifest"
)

// ConfigLayerHarness builds layers from bento.yaml layer definitions.
// Used when users define custom layers in their config.
type ConfigLayerHarness struct {
	layers []config.LayerConfig
}

func NewConfigLayerHarness(layers []config.LayerConfig) *ConfigLayerHarness {
	return &ConfigLayerHarness{layers: layers}
}

func (h *ConfigLayerHarness) Name() string            { return "custom" }
func (h *ConfigLayerHarness) Detect(_ string) bool    { return true }

func (h *ConfigLayerHarness) Layers(_ string) []LayerDef {
	defs := make([]LayerDef, 0, len(h.layers))
	for _, l := range h.layers {
		mediaType := manifest.MediaTypeForLayer(l.Name)
		catchAll := l.CatchAll || l.Name == "project"
		defs = append(defs, LayerDef{
			Name:      l.Name,
			Patterns:  l.Patterns,
			MediaType: mediaType,
			CatchAll:  catchAll,
		})
	}
	return defs
}

func (h *ConfigLayerHarness) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(h.Name(), workDir), nil
}

func (h *ConfigLayerHarness) Ignore() []string         { return CommonIgnorePatterns }
func (h *ConfigLayerHarness) SecretPatterns() []string  { return CommonSecretPatterns }
func (h *ConfigLayerHarness) DefaultHooks() map[string]string { return nil }
