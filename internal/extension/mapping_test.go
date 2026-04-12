package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- PortablePath tests ---

func TestPortablePath_HomeRelative(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	input := filepath.Join(home, ".claude", "projects", "test")
	got := PortablePath(input)
	if !strings.HasPrefix(got, "/~/") {
		t.Errorf("expected /~/ prefix, got %q", got)
	}
	if got != "/~/.claude/projects/test" {
		t.Errorf("expected /~/.claude/projects/test, got %q", got)
	}
}

func TestPortablePath_NonHomeAbsolute(t *testing.T) {
	got := PortablePath("/var/cache/build")
	if got != "/var/cache/build" {
		t.Errorf("expected /var/cache/build, got %q", got)
	}
}

func TestPortablePath_ForwardSlashes(t *testing.T) {
	// On all platforms, result should use forward slashes
	got := PortablePath("/some/path/with/slashes")
	if strings.Contains(got, "\\") {
		t.Errorf("expected forward slashes only, got %q", got)
	}
}

// --- ClaudeCode tests ---

func TestClaudeCodeProjectHash(t *testing.T) {
	// claudeProjectHash is deterministic from workDir
	hash := claudeProjectHash("/Users/alice/projects/myapp")
	if hash != "-Users-alice-projects-myapp" {
		t.Errorf("expected -Users-alice-projects-myapp, got %q", hash)
	}
}

func TestClaudeCodeNormalizePath(t *testing.T) {
	// Create a temp dir that mimics a Claude Code project directory
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	workDir := t.TempDir()

	// Compute the project hash for our temp workDir
	hash := claudeProjectHash(workDir)
	projectDir := filepath.Join(home, ".claude", "projects", hash)

	// Create the project directory so claudeProjectDir finds it
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(projectDir) })

	c := ClaudeCode{}
	normalize := c.NormalizePath(workDir)
	if normalize == nil {
		t.Fatal("expected non-nil normalizer")
	}

	// Test: matching path gets normalized
	input := "/~/.claude/projects/" + hash + "/memory/foo.md"
	got := normalize(input)
	expected := "/~/.claude/projects/__BENTO_WORKSPACE__/memory/foo.md"
	if got != expected {
		t.Errorf("normalize(%q) = %q, want %q", input, got, expected)
	}

	// Test: exact prefix match (no trailing path)
	input = "/~/.claude/projects/" + hash
	got = normalize(input)
	expected = "/~/.claude/projects/__BENTO_WORKSPACE__"
	if got != expected {
		t.Errorf("normalize(%q) = %q, want %q", input, got, expected)
	}

	// Test: non-matching path passes through unchanged
	input = "/~/.claude/settings.json"
	got = normalize(input)
	if got != input {
		t.Errorf("expected passthrough for %q, got %q", input, got)
	}
}

func TestClaudeCodeNormalizePathNoProjectDir(t *testing.T) {
	// workDir with no corresponding project dir
	workDir := t.TempDir()
	c := ClaudeCode{}
	normalize := c.NormalizePath(workDir)
	if normalize != nil {
		t.Error("expected nil normalizer when no project dir exists")
	}
}

func TestClaudeCodeResolvePath(t *testing.T) {
	c := ClaudeCode{}
	workDir := "/Users/bob/projects/myapp"
	resolve := c.ResolvePath(workDir)
	if resolve == nil {
		t.Fatal("expected non-nil resolver")
	}

	expectedHash := claudeProjectHash(workDir)

	// Test: placeholder gets expanded
	input := "/~/.claude/projects/__BENTO_WORKSPACE__/memory/foo.md"
	got := resolve(input)
	expected := "/~/.claude/projects/" + expectedHash + "/memory/foo.md"
	if got != expected {
		t.Errorf("resolve(%q) = %q, want %q", input, got, expected)
	}

	// Test: exact placeholder
	input = "/~/.claude/projects/__BENTO_WORKSPACE__"
	got = resolve(input)
	expected = "/~/.claude/projects/" + expectedHash
	if got != expected {
		t.Errorf("resolve(%q) = %q, want %q", input, got, expected)
	}

	// Test: non-placeholder path passes through
	input = "/~/.claude/settings.json"
	got = resolve(input)
	if got != input {
		t.Errorf("expected passthrough for %q, got %q", input, got)
	}
}

func TestClaudeCodeNormalizeResolveRoundtrip(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	// Machine A: workDir = tempDirA, saves a checkpoint
	workDirA := t.TempDir()
	hashA := claudeProjectHash(workDirA)
	projectDirA := filepath.Join(home, ".claude", "projects", hashA)
	if err := os.MkdirAll(projectDirA, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(projectDirA) })

	c := ClaudeCode{}

	// Normalize on Machine A
	normalize := c.NormalizePath(workDirA)
	if normalize == nil {
		t.Fatal("expected non-nil normalizer")
	}

	originalPath := "/~/.claude/projects/" + hashA + "/sessions/abc.jsonl"
	normalized := normalize(originalPath)

	if !strings.Contains(normalized, "__BENTO_WORKSPACE__") {
		t.Fatalf("expected __BENTO_WORKSPACE__ in normalized path, got %q", normalized)
	}

	// Machine B: different workDir, resolves the checkpoint
	workDirB := "/Users/bob/other/workspace"
	resolve := c.ResolvePath(workDirB)
	if resolve == nil {
		t.Fatal("expected non-nil resolver")
	}

	resolved := resolve(normalized)
	hashB := claudeProjectHash(workDirB)
	expected := "/~/.claude/projects/" + hashB + "/sessions/abc.jsonl"
	if resolved != expected {
		t.Errorf("roundtrip failed: got %q, want %q", resolved, expected)
	}
}

// --- Nil extensions return nil ---

func TestNilExtensionsReturnNil(t *testing.T) {
	nilExts := []Extension{
		Codex{}, Stakpak{}, AgentsMD{}, Node{},
		Python{}, GoMod{}, Rust{}, Ruby{}, Elixir{}, OCaml{},
		ToolVersions{},
		// OpenCode is excluded: it returns non-nil when the global
		// storage directory (~/.local/share/opencode/storage/) exists.
	}
	for _, ext := range nilExts {
		if fn := ext.NormalizePath("/tmp/test"); fn != nil {
			t.Errorf("%s.NormalizePath should return nil", ext.Name())
		}
		if fn := ext.ResolvePath("/tmp/test"); fn != nil {
			t.Errorf("%s.ResolvePath should return nil", ext.Name())
		}
	}
}
