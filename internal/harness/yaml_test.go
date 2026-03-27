package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kajogo777/bento/internal/config"
)

func TestNewYAMLHarness_Name(t *testing.T) {
	h := NewYAMLHarness(&config.InlineHarness{Name: "myharness"})
	if h.Name() != "myharness" {
		t.Fatalf("expected myharness, got %s", h.Name())
	}
}

func TestNewYAMLHarness_DefaultName(t *testing.T) {
	h := NewYAMLHarness(&config.InlineHarness{})
	if h.Name() != "custom" {
		t.Fatalf("expected custom, got %s", h.Name())
	}
}

func TestYAMLHarness_DetectWithFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".myagent"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewYAMLHarness(&config.InlineHarness{Detect: ".myagent"})
	if !h.Detect(dir) {
		t.Fatal("expected Detect to return true when detect file exists")
	}
}

func TestYAMLHarness_DetectWithMissingFile(t *testing.T) {
	dir := t.TempDir()

	h := NewYAMLHarness(&config.InlineHarness{Detect: ".missing"})
	if h.Detect(dir) {
		t.Fatal("expected Detect to return false when detect file is missing")
	}
}

func TestYAMLHarness_DetectEmptyString(t *testing.T) {
	dir := t.TempDir()

	h := NewYAMLHarness(&config.InlineHarness{Detect: ""})
	if !h.Detect(dir) {
		t.Fatal("expected Detect to return true when detect string is empty")
	}
}

func TestYAMLHarness_Layers(t *testing.T) {
	h := NewYAMLHarness(&config.InlineHarness{
		Layers: []config.InlineLayerDef{
			{
				Name:      "agent",
				Patterns:  []string{"*.md"},
				MediaType: "application/vnd.test.agent.v1",
				Frequency: "often",
			},
			{
				Name:      "deps",
				Patterns:  []string{"vendor/**"},
				MediaType: "application/vnd.test.deps.v1",
				Frequency: "rarely",
			},
		},
	})

	layers := h.Layers()
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(layers))
	}

	if layers[0].Name != "agent" {
		t.Errorf("first layer name = %q, want %q", layers[0].Name, "agent")
	}
	if layers[0].Frequency != ChangesOften {
		t.Errorf("first layer frequency = %q, want %q", layers[0].Frequency, ChangesOften)
	}
	if layers[1].Frequency != ChangesRarely {
		t.Errorf("second layer frequency = %q, want %q", layers[1].Frequency, ChangesRarely)
	}
}

func TestYAMLHarness_IgnoreCustom(t *testing.T) {
	custom := []string{"*.log", "tmp/**"}
	h := NewYAMLHarness(&config.InlineHarness{
		Ignore: custom,
	})

	ignore := h.Ignore()
	if len(ignore) != 2 {
		t.Fatalf("expected 2 ignore patterns, got %d", len(ignore))
	}
	if ignore[0] != "*.log" || ignore[1] != "tmp/**" {
		t.Errorf("ignore patterns = %v, want %v", ignore, custom)
	}
}

func TestYAMLHarness_IgnoreDefault(t *testing.T) {
	h := NewYAMLHarness(&config.InlineHarness{})

	ignore := h.Ignore()
	if len(ignore) == 0 {
		t.Fatal("expected default ignore patterns when none configured")
	}
}

func TestYAMLHarness_SecretPatternsCustom(t *testing.T) {
	patterns := []string{`SECRET_[A-Z]+`}
	h := NewYAMLHarness(&config.InlineHarness{
		SecretPatterns: patterns,
	})

	got := h.SecretPatterns()
	if len(got) != 1 || got[0] != patterns[0] {
		t.Errorf("SecretPatterns() = %v, want %v", got, patterns)
	}
}

func TestYAMLHarness_DefaultHooks(t *testing.T) {
	hooks := map[string]string{
		"post_restore": "make setup",
	}
	h := NewYAMLHarness(&config.InlineHarness{
		Hooks: hooks,
	})

	got := h.DefaultHooks()
	if got["post_restore"] != "make setup" {
		t.Errorf("DefaultHooks()[post_restore] = %q, want %q", got["post_restore"], "make setup")
	}
}
