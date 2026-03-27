package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kajogo777/bento/internal/harness"
)

// helper to create a file with content in a temp directory.
func createFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestScanAssignsFilesToLayers(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "src/main.go", "package main")
	createFile(t, dir, "src/util.go", "package main")
	createFile(t, dir, "go.mod", "module test")
	createFile(t, dir, "go.sum", "hash")
	createFile(t, dir, "README.md", "# readme")

	layers := []harness.LayerDef{
		{Name: "source", Patterns: []string{"src/**"}},
		{Name: "deps", Patterns: []string{"go.mod", "go.sum"}},
		{Name: "docs", Patterns: []string{"*.md"}},
	}

	s := NewScanner(dir, layers, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["source"]) != 2 {
		t.Errorf("source layer has %d files, want 2", len(result["source"]))
	}
	if len(result["deps"]) != 2 {
		t.Errorf("deps layer has %d files, want 2", len(result["deps"]))
	}
	if len(result["docs"]) != 1 {
		t.Errorf("docs layer has %d files, want 1", len(result["docs"]))
	}
}

func TestScanIgnoredFilesExcluded(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "src/main.go", "package main")
	createFile(t, dir, "src/debug.log", "log data")
	createFile(t, dir, ".DS_Store", "")

	layers := []harness.LayerDef{
		{Name: "source", Patterns: []string{"src/**"}},
		{Name: "meta", Patterns: []string{".DS_Store"}},
	}

	s := NewScanner(dir, layers, []string{"*.log", ".DS_Store"})
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	// debug.log should be ignored even though it matches src/**
	if len(result["source"]) != 1 {
		t.Errorf("source layer has %d files, want 1 (debug.log should be ignored)", len(result["source"]))
	}

	// .DS_Store should be ignored
	if len(result["meta"]) != 0 {
		t.Errorf("meta layer has %d files, want 0 (.DS_Store should be ignored)", len(result["meta"]))
	}
}

func TestScanUnmatchedFilesExcluded(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "src/main.go", "package main")
	createFile(t, dir, "random.xyz", "unknown")

	layers := []harness.LayerDef{
		{Name: "source", Patterns: []string{"src/**"}},
	}

	s := NewScanner(dir, layers, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if _, ok := result["source"]; !ok {
		t.Fatal("expected source layer in result")
	}
	if len(result["source"]) != 1 {
		t.Errorf("source layer has %d files, want 1", len(result["source"]))
	}

	// random.xyz matches no layer, should not appear anywhere.
	for layer, files := range result {
		for _, f := range files {
			if f == "random.xyz" {
				t.Errorf("random.xyz should not appear in any layer, found in %q", layer)
			}
		}
	}
}

func TestScanFirstLayerWins(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "src/main.go", "package main")

	layers := []harness.LayerDef{
		{Name: "first", Patterns: []string{"src/**"}},
		{Name: "second", Patterns: []string{"src/**"}},
	}

	s := NewScanner(dir, layers, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["first"]) != 1 {
		t.Errorf("first layer has %d files, want 1", len(result["first"]))
	}
	if len(result["second"]) != 0 {
		t.Errorf("second layer has %d files, want 0 (first layer should win)", len(result["second"]))
	}
}
