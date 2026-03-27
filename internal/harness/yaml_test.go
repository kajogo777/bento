package harness

import (
	"testing"

	"github.com/kajogo777/bento/internal/config"
)

func TestNewConfigLayerHarness_Name(t *testing.T) {
	h := NewConfigLayerHarness(nil)
	if h.Name() != "custom" {
		t.Fatalf("expected custom, got %s", h.Name())
	}
}

func TestConfigLayerHarness_DetectAlwaysTrue(t *testing.T) {
	h := NewConfigLayerHarness(nil)
	if !h.Detect("") {
		t.Fatal("expected Detect to always return true")
	}
}

func TestConfigLayerHarness_Layers(t *testing.T) {
	h := NewConfigLayerHarness([]config.LayerConfig{
		{
			Name:     "agent",
			Patterns: []string{"*.md"},
		},
		{
			Name:     "deps",
			Patterns: []string{"vendor/**"},
		},
	})

	layers := h.Layers("")
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(layers))
	}

	if layers[0].Name != "agent" {
		t.Errorf("first layer name = %q, want %q", layers[0].Name, "agent")
	}
	if layers[1].Name != "deps" {
		t.Errorf("second layer name = %q, want %q", layers[1].Name, "deps")
	}
}

func TestConfigLayerHarness_IgnoreDefault(t *testing.T) {
	h := NewConfigLayerHarness(nil)

	ignore := h.Ignore()
	if len(ignore) == 0 {
		t.Fatal("expected default ignore patterns when none configured")
	}
}

func TestConfigLayerHarness_SecretPatternsDefault(t *testing.T) {
	h := NewConfigLayerHarness(nil)

	got := h.SecretPatterns()
	if len(got) == 0 {
		t.Fatal("expected default secret patterns")
	}
}

func TestConfigLayerHarness_DefaultHooksNil(t *testing.T) {
	h := NewConfigLayerHarness(nil)

	got := h.DefaultHooks()
	if got != nil {
		t.Errorf("DefaultHooks() = %v, want nil", got)
	}
}
