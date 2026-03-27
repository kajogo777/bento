package harness

import (
	"os"
	"path/filepath"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/manifest"
)

// YAMLHarness implements the Harness interface from an inline YAML configuration.
type YAMLHarness struct {
	cfg *config.InlineHarness
}

// NewYAMLHarness creates a Harness from an InlineHarness configuration.
func NewYAMLHarness(cfg *config.InlineHarness) *YAMLHarness {
	return &YAMLHarness{cfg: cfg}
}

func (y *YAMLHarness) Name() string {
	if y.cfg.Name != "" {
		return y.cfg.Name
	}
	return "custom"
}

func (y *YAMLHarness) Detect(workDir string) bool {
	if y.cfg.Detect == "" {
		return true
	}
	path := filepath.Join(workDir, y.cfg.Detect)
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func (y *YAMLHarness) Layers() []LayerDef {
	layers := make([]LayerDef, 0, len(y.cfg.Layers))
	for _, l := range y.cfg.Layers {
		mediaType := l.MediaType
		if mediaType == "" {
			mediaType = manifest.MediaTypeForLayer(l.Name)
		}
		layers = append(layers, LayerDef{
			Name:      l.Name,
			Patterns:  l.Patterns,
			MediaType: mediaType,
			Frequency: mapFrequency(l.Frequency),
		})
	}
	return layers
}

func (y *YAMLHarness) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(y.Name(), workDir), nil
}

func (y *YAMLHarness) Ignore() []string {
	if len(y.cfg.Ignore) > 0 {
		return y.cfg.Ignore
	}
	return CommonIgnorePatterns
}

func (y *YAMLHarness) SecretPatterns() []string {
	if len(y.cfg.SecretPatterns) > 0 {
		return y.cfg.SecretPatterns
	}
	return CommonSecretPatterns
}

func (y *YAMLHarness) DefaultHooks() map[string]string {
	return y.cfg.Hooks
}

// mapFrequency converts a string frequency value to the ChangeFrequency type.
func mapFrequency(s string) ChangeFrequency {
	switch s {
	case "often":
		return ChangesOften
	case "rarely":
		return ChangesRarely
	default:
		return ChangesOften
	}
}
