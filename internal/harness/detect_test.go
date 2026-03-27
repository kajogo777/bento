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
		t.Fatalf("expected claude-code, got %s", h.Name())
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

func TestDetect_OpenCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "opencode" {
		t.Fatalf("expected opencode, got %s", h.Name())
	}
}

func TestDetect_OpenCode_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "opencode" {
		t.Fatalf("expected opencode, got %s", h.Name())
	}
}

func TestDetect_OpenClaw(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("# soul"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "openclaw" {
		t.Fatalf("expected openclaw, got %s", h.Name())
	}
}

func TestDetect_OpenClaw_Identity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte("# id"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "openclaw" {
		t.Fatalf("expected openclaw, got %s", h.Name())
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

func TestDetect_Fallback(t *testing.T) {
	dir := t.TempDir()

	h := Detect(dir)
	if h.Name() != "auto" {
		t.Fatalf("expected auto (fallback), got %s", h.Name())
	}
}

func TestDetect_MultipleAgents_Composite(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := Detect(dir)
	if h.Name() != "claude-code+codex" {
		t.Fatalf("expected composite claude-code+codex, got %s", h.Name())
	}
	layers := h.Layers(dir)
	if len(layers) < 4 {
		t.Fatalf("expected at least 4 layers (2 agent + deps + project), got %d", len(layers))
	}
}

func TestDetectSingle(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := DetectSingle(dir, "claude-code")
	if h.Name() != "claude-code" {
		t.Fatalf("expected claude-code, got %s", h.Name())
	}
}

func TestHarness_Names(t *testing.T) {
	tests := []struct {
		harness Harness
		want    string
	}{
		{ClaudeCode{}, "claude-code"},
		{Codex{}, "codex"},
		{OpenCode{}, "opencode"},
		{OpenClaw{}, "openclaw"},
		{Cursor{}, "cursor"},
		{Fallback{}, "auto"},
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
		OpenCode{},
		OpenClaw{},
		Cursor{},
		Fallback{},
	}

	for _, h := range harnesses {
		layers := h.Layers("")
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

func TestHarness_IgnoreIncludesCredentials(t *testing.T) {
	harnesses := []Harness{
		ClaudeCode{},
		Codex{},
		OpenCode{},
		OpenClaw{},
		Cursor{},
	}

	for _, h := range harnesses {
		patterns := h.Ignore()
		found := false
		for _, p := range patterns {
			if p == "auth.json" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: Ignore() missing credential file 'auth.json'", h.Name())
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

func TestCodex_RequiresCodexDir(t *testing.T) {
	dir := t.TempDir()
	// AGENTS.md alone should NOT trigger Codex (ambiguous with OpenCode)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# agents"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Codex{}
	if h.Detect(dir) {
		t.Fatal("expected Codex.Detect to return false for AGENTS.md alone")
	}

	// .codex/ dir should trigger
	if err := os.Mkdir(filepath.Join(dir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !h.Detect(dir) {
		t.Fatal("expected Codex.Detect to return true for .codex/ dir")
	}
}

func TestFallback_AlwaysDetects(t *testing.T) {
	dir := t.TempDir()

	h := Fallback{}
	if !h.Detect(dir) {
		t.Fatal("expected Fallback.Detect to always return true")
	}
}
