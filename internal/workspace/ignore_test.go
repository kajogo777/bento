package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnoreMatcherSimplePatterns(t *testing.T) {
	m := NewIgnoreMatcher([]string{"*.log", ".DS_Store"})

	tests := []struct {
		path string
		want bool
	}{
		{"app.log", true},
		{"logs/debug.log", true},
		{".DS_Store", true},
		{"sub/.DS_Store", true},
		{"main.go", false},
		{"readme.md", false},
	}

	for _, tt := range tests {
		got := m.Match(tt.path)
		if got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIgnoreMatcherGlobstar(t *testing.T) {
	m := NewIgnoreMatcher([]string{".git/**", "node_modules/**"})

	tests := []struct {
		path string
		want bool
	}{
		{".git/config", true},
		{".git/objects/abc123", true},
		{".git", true}, // exact match on prefix
		{"node_modules/express/index.js", true},
		{"node_modules", true},
		{"src/main.go", false},
		{"gitignore", false},
	}

	for _, tt := range tests {
		got := m.Match(tt.path)
		if got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIgnoreMatcherDoubleStarPrefix(t *testing.T) {
	m := NewIgnoreMatcher([]string{"**/*.tmp"})

	tests := []struct {
		path string
		want bool
	}{
		{"file.tmp", true},
		{"sub/file.tmp", true},
		{"a/b/c/file.tmp", true},
		{"file.txt", false},
	}

	for _, tt := range tests {
		got := m.Match(tt.path)
		if got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIgnoreMatcherNoMatch(t *testing.T) {
	m := NewIgnoreMatcher([]string{"*.log", "build/**"})

	paths := []string{"main.go", "src/app.ts", "README.md"}
	for _, p := range paths {
		if m.Match(p) {
			t.Errorf("Match(%q) = true, want false", p)
		}
	}
}

func TestIgnoreMatcherEmpty(t *testing.T) {
	m := NewIgnoreMatcher(nil)
	if m.Match("anything.go") {
		t.Error("empty matcher should not match any path")
	}
}

func TestLoadBentoIgnore(t *testing.T) {
	dir := t.TempDir()
	content := `# This is a comment
*.log
.DS_Store

# Another comment
node_modules/**
`
	if err := os.WriteFile(filepath.Join(dir, ".bentoignore"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	patterns, err := LoadBentoIgnore(dir)
	if err != nil {
		t.Fatalf("LoadBentoIgnore returned error: %v", err)
	}

	expected := []string{"*.log", ".DS_Store", "node_modules/**"}
	if len(patterns) != len(expected) {
		t.Fatalf("got %d patterns, want %d", len(patterns), len(expected))
	}
	for i, p := range patterns {
		if p != expected[i] {
			t.Errorf("pattern[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

func TestLoadBentoIgnoreMissingFile(t *testing.T) {
	dir := t.TempDir()
	patterns, err := LoadBentoIgnore(dir)
	if err != nil {
		t.Fatalf("LoadBentoIgnore returned error for missing file: %v", err)
	}
	if patterns != nil {
		t.Errorf("expected nil patterns for missing file, got %v", patterns)
	}
}
