package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect_ClaudeCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "claude-code" {
		t.Fatalf("expected claudecode, got %s", h.Name())
	}
}

func TestDetect_Codex(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "codex" {
		t.Fatalf("expected codex, got %s", h.Name())
	}
}

func TestDetect_Aider(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".aider.conf.yml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "aider" {
		t.Fatalf("expected aider, got %s", h.Name())
	}
}

func TestDetect_Cursor(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "cursor" {
		t.Fatalf("expected cursor, got %s", h.Name())
	}
}

func TestDetect_Windsurf(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".windsurf"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "windsurf" {
		t.Fatalf("expected windsurf, got %s", h.Name())
	}
}

func TestDetect_Fallback(t *testing.T) {
	dir := t.TempDir()

	h := Detect(dir)
	if h.Name() != "default" {
		t.Fatalf("expected default (fallback), got %s", h.Name())
	}
}

func TestDetect_Priority_ClaudeCodeOverCodex(t *testing.T) {
	dir := t.TempDir()
	// Create both .claude and .codex; ClaudeCode should win (first in list).
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "claude-code" {
		t.Fatalf("expected claudecode to win priority, got %s", h.Name())
	}
}

func TestHarness_Names(t *testing.T) {
	tests := []struct {
		harness Harness
		want    string
	}{
		{ClaudeCode{}, "claude-code"},
		{Codex{}, "codex"},
		{Aider{}, "aider"},
		{Cursor{}, "cursor"},
		{Windsurf{}, "windsurf"},
		{Fallback{}, "default"},
	}

	for _, tt := range tests {
		if got := tt.harness.Name(); got != tt.want {
			t.Errorf("Name() = %q, want %q", got, tt.want)
		}
	}
}

func TestHarness_LayersNonEmpty(t *testing.T) {
	harnesses := []Harness{
		ClaudeCode{},
		Codex{},
		Aider{},
		Cursor{},
		Windsurf{},
		Fallback{},
	}

	for _, h := range harnesses {
		layers := h.Layers()
		if len(layers) == 0 {
			t.Errorf("%s: Layers() returned empty slice", h.Name())
		}
		for _, l := range layers {
			if l.Name == "" {
				t.Errorf("%s: layer has empty name", h.Name())
			}
			if l.MediaType == "" {
				t.Errorf("%s: layer %q has empty MediaType", h.Name(), l.Name)
			}
		}
	}
}

func TestClaudeCode_DetectsClaudeMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# claude"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := ClaudeCode{}
	if !h.Detect(dir) {
		t.Fatal("expected ClaudeCode.Detect to return true for CLAUDE.md")
	}
}

func TestCodex_DetectsAgentsMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# agents"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Codex{}
	if !h.Detect(dir) {
		t.Fatal("expected Codex.Detect to return true for AGENTS.md")
	}
}

func TestFallback_AlwaysDetects(t *testing.T) {
	dir := t.TempDir()

	h := Fallback{}
	if !h.Detect(dir) {
		t.Fatal("expected Fallback.Detect to always return true")
	}
}
