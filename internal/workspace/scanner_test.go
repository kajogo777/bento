package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kajogo777/bento/internal/harness"
)

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

	if len(result["source"].WorkspaceFiles) != 2 {
		t.Errorf("source layer has %d files, want 2", len(result["source"].WorkspaceFiles))
	}
	if len(result["deps"].WorkspaceFiles) != 2 {
		t.Errorf("deps layer has %d files, want 2", len(result["deps"].WorkspaceFiles))
	}
	if len(result["docs"].WorkspaceFiles) != 1 {
		t.Errorf("docs layer has %d files, want 1", len(result["docs"].WorkspaceFiles))
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

	if len(result["source"].WorkspaceFiles) != 1 {
		t.Errorf("source layer has %d files, want 1 (debug.log should be ignored)", len(result["source"].WorkspaceFiles))
	}
	if len(result["meta"].WorkspaceFiles) != 0 {
		t.Errorf("meta layer has %d files, want 0 (.DS_Store should be ignored)", len(result["meta"].WorkspaceFiles))
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

	if len(result["source"].WorkspaceFiles) != 1 {
		t.Errorf("source layer has %d files, want 1", len(result["source"].WorkspaceFiles))
	}

	for layer, sr := range result {
		for _, f := range sr.WorkspaceFiles {
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

	if len(result["first"].WorkspaceFiles) != 1 {
		t.Errorf("first layer has %d files, want 1", len(result["first"].WorkspaceFiles))
	}
	if len(result["second"].WorkspaceFiles) != 0 {
		t.Errorf("second layer has %d files, want 0 (first layer should win)", len(result["second"].WorkspaceFiles))
	}
}

func TestScanExternalPatterns(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	createFile(t, dir, "main.go", "package main")
	createFile(t, extDir, "session.jsonl", "data")
	createFile(t, extDir, "sub/notes.txt", "notes")

	layers := []harness.LayerDef{
		{Name: "agent", Patterns: []string{extDir + "/"}},
		{Name: "project", Patterns: []string{"**"}, CatchAll: true},
	}

	s := NewScanner(dir, layers, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["agent"].ExternalFiles) != 2 {
		t.Errorf("agent layer has %d external files, want 2", len(result["agent"].ExternalFiles))
	}
	if len(result["project"].WorkspaceFiles) != 1 {
		t.Errorf("project layer has %d workspace files, want 1", len(result["project"].WorkspaceFiles))
	}

	// Verify external files have __external__ prefix in archive path
	for _, ef := range result["agent"].ExternalFiles {
		if ef.AbsPath == "" {
			t.Error("external file has empty AbsPath")
		}
		if len(ef.ArchivePath) < 13 || ef.ArchivePath[:13] != "__external__/" {
			t.Errorf("external file archive path missing __external__ prefix: %s", ef.ArchivePath)
		}
	}
}

func TestScanRejectsPathTraversalInExternalPatterns(t *testing.T) {
	dir := t.TempDir()

	layers := []harness.LayerDef{
		{Name: "agent", Patterns: []string{"~/../../etc/passwd"}},
	}

	s := NewScanner(dir, layers, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["agent"].ExternalFiles) != 0 {
		t.Errorf("agent layer should have 0 external files (.. rejected), got %d", len(result["agent"].ExternalFiles))
	}
}
